package native

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// pathInput matches input names that usually carry something to open. It is
// an inference from names, not from behaviour; the permissions matrix
// (internal/permsmatrix) and the load-time path rule share it so they cannot
// disagree about which tools they mean.
var pathInput = regexp.MustCompile(`^(name|page)$|(^|[-_])(path|paths|file|files|dir|dirs|cwd|folder|root|source|destination|target|installable|flake_dir|repo_path|session_id|session_ids)$`)

// PathLikeInputs returns, sorted, the inputs of the schema whose name looks
// like a path, directory, repo or session id.
func PathLikeInputs(in *InputSchema) []string {
	if in == nil {
		return nil
	}
	var out []string
	for name := range in.Properties {
		if pathInput.MatchString(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// checkPathRule enforces moxy#520: an always-allow tool whose inputs look
// like paths must either name them in [resources.*], so the policy broker and
// the floor judge what they point at, or carry a path-exempt reason saying
// why nothing needs to. A path-exempt on a tool the rule does not cover is
// refused too, so a stale exemption cannot hide the next real gap.
func checkPathRule(tier PermsRequest, in *InputSchema, resources []ResourceArg, exempt string) error {
	covered := tier == PermsAlwaysAllow && len(resources) == 0 && len(PathLikeInputs(in)) > 0
	hasExempt := exempt != ""
	switch {
	case hasExempt && strings.TrimSpace(exempt) == "":
		return fmt.Errorf("path-exempt must give a reason")
	case covered && !hasExempt:
		return fmt.Errorf(
			"always-allow tool takes path-like input(s) %s but declares no [resources.<input>] stanza; add one, or set path-exempt = \"<why nothing needs to judge it>\"",
			strings.Join(PathLikeInputs(in), ", "))
	case hasExempt && !covered:
		return fmt.Errorf("path-exempt is set but the tool is not an always-allow path reader without [resources]; remove it")
	}
	return nil
}
