package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	miropsv1 "github.com/miropshq/mirops/api/v1"
	"github.com/miropshq/mirops/internal/analysis"
	"github.com/miropshq/mirops/internal/graph"
)

type aiScoreResponse struct {
	Score     int             `json:"score"`
	Reasoning string          `json:"reasoning"`
	Actions   []aiActionEntry `json:"actions,omitempty"`
}

type aiActionEntry struct {
	Type      string            `json:"type"`
	Namespace string            `json:"namespace,omitempty"`
	Name      string            `json:"name"`
	Reason    string            `json:"reason"`
	Risk      string            `json:"risk"`
	Params    map[string]string `json:"params,omitempty"`
}

// aiCacheEntry is the last AI result for one UpgradeAnalysis, keyed by a hash of everything the call
// depends on (provider + model + prompt). Reused when a later resync produces the identical hash.
type aiCacheEntry struct {
	hash      string
	score     int
	reasoning string
	actions   []aiActionEntry
}

func (r *UpgradeAnalysisReconciler) aiCacheGet(key, hash string) (aiCacheEntry, bool) {
	r.aiCacheMu.Lock()
	defer r.aiCacheMu.Unlock()
	if e, ok := r.aiCache[key]; ok && e.hash == hash {
		return e, true
	}
	return aiCacheEntry{}, false
}

func (r *UpgradeAnalysisReconciler) aiCachePut(key string, e aiCacheEntry) {
	r.aiCacheMu.Lock()
	defer r.aiCacheMu.Unlock()
	if r.aiCache == nil {
		r.aiCache = make(map[string]aiCacheEntry)
	}
	r.aiCache[key] = e
}

// scoreWithAI calls the configured AI provider and returns score, reasoning, actions and error.
// A resync on a steady cluster rebuilds the exact same prompt; rather than pay for an identical
// answer every interval, the result is memoized per UpgradeAnalysis and reused until the prompt
// (i.e. the cluster state the model sees) actually changes.
func (r *UpgradeAnalysisReconciler) scoreWithAI(ctx context.Context, ua *miropsv1.UpgradeAnalysis, report *analysis.Report) (int, string, []aiActionEntry, error) {
	apiKey, err := r.readAIAPIKey(ctx, ua)
	if err != nil {
		return 0, "", nil, err
	}

	model := ua.Spec.AI.Model
	prompt := buildAIPrompt(report, ua.Spec.AI.Remediation.Enabled)

	// Cache key covers everything that changes the answer: provider, model, and the prompt itself.
	sum := sha256.Sum256([]byte(string(ua.Spec.AI.Provider) + "\x00" + model + "\x00" + prompt))
	hash := hex.EncodeToString(sum[:])
	if cached, ok := r.aiCacheGet(ua.Name, hash); ok {
		logf.FromContext(ctx).V(1).Info("reusing cached AI result (cluster state unchanged since last run)")
		return cached.score, cached.reasoning, cached.actions, nil
	}

	var raw string
	switch ua.Spec.AI.Provider {
	case miropsv1.AIProviderOpenAI:
		raw, err = callOpenAI(ctx, apiKey, model, prompt)
	default:
		raw, err = callAnthropic(ctx, apiKey, model, prompt)
	}
	if err != nil {
		return 0, "", nil, err
	}

	score, reasoning, actions, err := parseAIResponse(raw)
	if err != nil {
		return 0, "", nil, err // don't cache a failed parse
	}
	r.aiCachePut(ua.Name, aiCacheEntry{hash: hash, score: score, reasoning: reasoning, actions: actions})
	return score, reasoning, actions, nil
}

func callAnthropic(ctx context.Context, apiKey, model, prompt string) (string, error) {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 2048,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("calling Anthropic API: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic")
	}
	return msg.Content[0].Text, nil
}

func callOpenAI(ctx context.Context, apiKey, model, prompt string) (string, error) {
	if model == "" {
		model = "gpt-4o"
	}
	client := openai.NewClient(openaioption.WithAPIKey(apiKey))
	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("calling OpenAI API: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from OpenAI")
	}
	return resp.Choices[0].Message.Content, nil
}

func (r *UpgradeAnalysisReconciler) readAIAPIKey(ctx context.Context, ua *miropsv1.UpgradeAnalysis) (string, error) {
	if ua.Spec.AI.CredentialsSecret == "" {
		return "", fmt.Errorf("ai.credentialsSecret is required when ai.enabled is true")
	}
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      ua.Spec.AI.CredentialsSecret,
		Namespace: r.OperatorNamespace,
	}, secret); err != nil {
		return "", fmt.Errorf("reading AI credentials secret %q: %w", ua.Spec.AI.CredentialsSecret, err)
	}

	keyName := "ANTHROPIC_API_KEY"
	if ua.Spec.AI.Provider == miropsv1.AIProviderOpenAI {
		keyName = "OPENAI_API_KEY"
	}
	key := string(secret.Data[keyName])
	if key == "" {
		return "", fmt.Errorf("%s not found in secret %q", keyName, ua.Spec.AI.CredentialsSecret)
	}
	return key, nil
}

func buildAIPrompt(report *analysis.Report, withRemediation bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a Kubernetes upgrade risk assessor. Analyze the cluster state and return an upgrade readiness score from 0 to 100.\n\n")
	fmt.Fprintf(&b, "100 = completely safe, 0 = critical risk. Focus on semantic risks that numeric metrics miss:\n")
	fmt.Fprintf(&b, "- Workload criticality from names (postgres, kafka, payment suggest production data)\n")
	fmt.Fprintf(&b, "- Data loss risk during node drain for stateful workloads\n")
	fmt.Fprintf(&b, "- Active jobs or migrations that would be interrupted\n")
	fmt.Fprintf(&b, "- Correlated failures across workloads\n")
	fmt.Fprintf(&b, "- Add-on incompatibility with the target version and what depends on those add-ons (see ADD-ON COMPATIBILITY and KEY DEPENDENCIES). Treat the rule-based compatibility as a hint: confirm it and flag add-ons or dependency chains the rules may have missed.\n\n")

	fmt.Fprintf(&b, "CLUSTER:\n")
	fmt.Fprintf(&b, "Current version: %s | Target: %s\n", report.ClusterVersion, report.TargetVersion)
	fmt.Fprintf(&b, "Base score: %d/100 | Decision: %s\n", report.Scores.Base.Score, report.Decision.Level)
	fmt.Fprintf(&b, "Reason: %s\n\n", report.Reason)

	if len(report.Issues) > 0 {
		fmt.Fprintf(&b, "ISSUES:\n")
		for _, issue := range report.Issues {
			fmt.Fprintf(&b, "- %s\n", issue)
		}
		fmt.Fprintf(&b, "\n")
	}

	// Only send rows that carry a risk signal. A Ready node and a fully-ready Deployment tell the
	// model nothing it can act on, and on a large cluster the healthy majority would dominate the
	// prompt — so we drop them. (StatefulSets/Jobs already arrive problem-only from the operator.)
	notReadyNodes := 0
	for _, n := range report.Workloads.Nodes {
		if n.Status != "Ready" {
			notReadyNodes++
		}
	}
	if notReadyNodes > 0 {
		fmt.Fprintf(&b, "NODES (not Ready):\n")
		for _, n := range report.Workloads.Nodes {
			if n.Status == "Ready" {
				continue
			}
			fmt.Fprintf(&b, "- %s: %s\n", n.Name, n.Status)
		}
		fmt.Fprintf(&b, "\n")
	}

	notReadyDeps := 0
	for _, d := range report.Workloads.Deployments {
		if d.ReadyReplicas < d.DesiredReplicas {
			notReadyDeps++
		}
	}
	if notReadyDeps > 0 {
		fmt.Fprintf(&b, "DEPLOYMENTS (not ready):\n")
		for _, d := range report.Workloads.Deployments {
			if d.ReadyReplicas >= d.DesiredReplicas {
				continue
			}
			fmt.Fprintf(&b, "- %s/%s: %d/%d ready\n", d.Namespace, d.Name, d.ReadyReplicas, d.DesiredReplicas)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(report.Workloads.StatefulSets) > 0 {
		fmt.Fprintf(&b, "STATEFULSETS (issues):\n")
		for _, ss := range report.Workloads.StatefulSets {
			fmt.Fprintf(&b, "- %s/%s: %d/%d ready\n", ss.Namespace, ss.Name, ss.ReadyReplicas, ss.DesiredReplicas)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(report.Workloads.Jobs) > 0 {
		fmt.Fprintf(&b, "JOBS WITH ISSUES:\n")
		for _, j := range report.Workloads.Jobs {
			if j.Status == "Failed" {
				fmt.Fprintf(&b, "- %s/%s: failed (%s)\n", j.Namespace, j.Name, j.Reason)
			} else {
				fmt.Fprintf(&b, "- %s/%s: %d active pods\n", j.Namespace, j.Name, j.Active)
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(report.Workloads.DeprecatedAPIs) > 0 {
		fmt.Fprintf(&b, "DEPRECATED APIS:\n")
		for _, api := range report.Workloads.DeprecatedAPIs {
			fmt.Fprintf(&b, "- %s (%s) removed in k8s %s\n", api.Resource, api.Version, api.RemovedIn)
		}
		fmt.Fprintf(&b, "\n")
	}

	// Mirops engine context: add-on compatibility, dependency graph and risk give the AI
	// a logical mirror of the cluster to reason over, instead of raw metrics alone.
	if len(report.Addons) > 0 {
		fmt.Fprintf(&b, "ADD-ON COMPATIBILITY (rule-based, verify and add semantic judgement):\n")
		for _, a := range report.Addons {
			line := fmt.Sprintf("- %s %s: %s", a.Name, a.Version, a.Status)
			if a.RequiredVersion != "" {
				line += fmt.Sprintf(" (supported on k8s %s)", a.RequiredVersion)
			}
			fmt.Fprintf(&b, "%s\n", line)
		}
		fmt.Fprintf(&b, "\n")
	}

	if report.Risk != nil {
		riskyNamespaces := 0
		for _, ns := range report.Risk.ByNamespace {
			if ns.Risk > 0 {
				riskyNamespaces++
			}
		}
		if riskyNamespaces > 0 {
			fmt.Fprintf(&b, "RISK BY NAMESPACE (0-100, higher = riskier):\n")
			for _, ns := range report.Risk.ByNamespace {
				if ns.Risk == 0 {
					continue
				}
				fmt.Fprintf(&b, "- %s: risk %d (%d/%d components at risk)\n", ns.Namespace, ns.Risk, ns.AtRisk, ns.Components)
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	writeGraphContext(&b, report.Graph)

	if withRemediation {
		fmt.Fprintf(&b, "Also propose remediation actions for the detected issues.\n")
		fmt.Fprintf(&b, "Valid action types: restart-pod, scale-deployment, cordon-node, delete-pod\n")
		fmt.Fprintf(&b, "Valid risk levels: low, medium, high\n\n")
		fmt.Fprintf(&b, "Respond ONLY with JSON, no markdown:\n")
		fmt.Fprintf(&b, `{"score": <integer 0-100>, "reasoning": "<one concise paragraph>", "actions": [{"type": "<type>", "namespace": "<ns>", "name": "<name>", "reason": "<why>", "risk": "<low|medium|high>"}]}`)
	} else {
		fmt.Fprintf(&b, "Respond ONLY with JSON, no markdown:\n")
		fmt.Fprintf(&b, `{"score": <integer 0-100>, "reasoning": "<one concise paragraph>"}`)
	}
	return b.String()
}

// writeGraphContext appends per-component risk and dependency blast-radius sections to the AI
// prompt, giving the model the specific risky components (id + type + status) and the edges
// touching them — not just the namespace aggregate. Both sections are bounded to keep the prompt
// small, and omitted when empty (no risky components, or no operator graph).
func writeGraphContext(b *strings.Builder, g *graph.Graph) {
	if g == nil {
		return
	}
	atRisk := make([]graph.Component, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Risk > 0 {
			atRisk = append(atRisk, n)
		}
	}
	sort.Slice(atRisk, func(i, j int) bool { return atRisk[i].Risk > atRisk[j].Risk })

	if len(atRisk) > 0 {
		const maxComponents = 30
		fmt.Fprintf(b, "COMPONENTS AT RISK (0-100, propagated through dependencies):\n")
		for i, n := range atRisk {
			if i >= maxComponents {
				fmt.Fprintf(b, "- ...and %d more\n", len(atRisk)-maxComponents)
				break
			}
			status := n.Status
			if status == "" {
				status = "-"
			}
			fmt.Fprintf(b, "- %s (%s, status %s): risk %d\n", n.ID, n.Type, status, n.Risk)
		}
		fmt.Fprintf(b, "\n")
	}

	// Dependency blast radius: edges touching an at-risk component (either endpoint), across all
	// edge types — what a risky component affects, or what it hangs off of.
	risky := make(map[string]bool, len(atRisk))
	for _, n := range atRisk {
		if n.Risk >= 50 {
			risky[n.ID] = true
		}
	}
	deps := make([]string, 0, 20)
	for _, e := range g.Edges {
		if risky[e.From] || risky[e.To] {
			deps = append(deps, fmt.Sprintf("- %s %s %s", e.From, e.Type, e.To))
			if len(deps) >= 20 {
				break
			}
		}
	}
	if len(deps) > 0 {
		fmt.Fprintf(b, "KEY DEPENDENCIES (edges touching an at-risk component):\n")
		for _, d := range deps {
			fmt.Fprintf(b, "%s\n", d)
		}
		fmt.Fprintf(b, "\n")
	}
}

// classifyAIError turns a raw SDK/API error into a clear, user-facing message that
// explains the cause (invalid API key, insufficient credit, rate limit, missing model,
// etc.). It falls back to the raw error for non-API errors (config, parsing, network).
func classifyAIError(provider miropsv1.AIProvider, err error) string {
	if err == nil {
		return ""
	}

	switch provider {
	case miropsv1.AIProviderOpenAI:
		var oe *openai.Error
		if errors.As(err, &oe) {
			return formatAPIError("OpenAI", oe.StatusCode, oe.Type, oe.Message)
		}
	default:
		var ae *anthropic.Error
		if errors.As(err, &ae) {
			return formatAPIError("Anthropic", ae.StatusCode, string(ae.Type()), extractAnthropicMessage(ae.RawJSON()))
		}
	}

	// Not a structured API error (e.g. missing secret, JSON parse error, network failure)
	return err.Error()
}

// formatAPIError maps an HTTP status / error type / message into a human-readable cause.
func formatAPIError(provider string, status int, errType, message string) string {
	lowerMsg := strings.ToLower(message)
	var category string
	switch {
	case status == 401 || strings.Contains(errType, "authentication"):
		category = "invalid or unauthorized API key"
	case status == 403 || strings.Contains(errType, "permission"):
		category = "permission denied for this model or account"
	case strings.Contains(lowerMsg, "credit") || strings.Contains(lowerMsg, "billing") || strings.Contains(lowerMsg, "quota") || strings.Contains(lowerMsg, "insufficient"):
		category = "insufficient credit/quota on the AI account"
	case status == 429 || strings.Contains(errType, "rate_limit"):
		category = "rate limit exceeded — retry later"
	case status == 404 || strings.Contains(errType, "not_found"):
		category = "model not found — check spec.ai.model"
	case status == 529 || strings.Contains(errType, "overloaded"):
		category = "AI service overloaded — retry later"
	case status >= 500:
		category = "AI service internal error"
	default:
		category = "AI API call failed"
	}

	if message != "" {
		return fmt.Sprintf("%s: %s [%s HTTP %d]", category, message, provider, status)
	}
	return fmt.Sprintf("%s [%s HTTP %d]", category, provider, status)
}

// extractAnthropicMessage pulls the human-readable message out of the Anthropic error body.
func extractAnthropicMessage(raw string) string {
	if raw == "" {
		return ""
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err == nil {
		return body.Error.Message
	}
	return ""
}

func parseAIResponse(text string) (int, string, []aiActionEntry, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var resp aiScoreResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return 0, "", nil, fmt.Errorf("parsing AI response: %w (raw: %s)", err, text)
	}
	if resp.Score < 0 || resp.Score > 100 {
		return 0, "", nil, fmt.Errorf("AI score %d out of range [0, 100]", resp.Score)
	}
	return resp.Score, resp.Reasoning, resp.Actions, nil
}
