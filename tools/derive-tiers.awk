# Derives a kind and a tier for each tool row of docs/permissions-matrix.md and
# reports where the derived tier differs from the declared one. Run through
# `just debug-derive-tiers` (awk -F'|').
#
# Columns after -F'|': $2 tool, $3 tier, $6 resources, $8 hints.
function trim(s) {
	gsub(/^[ \t`]+|[ \t`]+$/, "", s)
	return s
}
/^\| `[a-z]/ {
	tool = trim($2)
	declared = trim($3)
	ro = ($8 ~ /read-only=true/)
	hasres = ($6 ~ /:/)
	kind = ro ? "read" : "write"
	if (hasres) {
		derived = "dynamic"
	} else if (ro) {
		derived = "always-allow"
	} else {
		derived = "delegate-to-client"
	}
	total++
	kinds[kind]++
	pair[declared " -> " derived]++
	if (declared != derived) {
		why = ""
		if (kind == "write" && declared == "always-allow") {
			why = "writes by hint but declared always-allow"
		} else if (kind == "read" && declared == "dynamic" && !hasres) {
			why = "read-only by hint, dynamic with no [resources] (a code predicate)"
		} else if (kind == "write" && declared == "dynamic" && !hasres) {
			why = "write by hint, dynamic with no [resources] (a code predicate)"
		} else if (kind == "read" && declared == "always-allow") {
			why = "read-only but derived dynamic from [resources]"
		} else {
			why = "other"
		}
		diff[++nd] = sprintf("%-28s declared %-18s derived %-18s kind %-5s %s", tool, declared, derived, kind, why)
		reasons[why]++
	}
}
END {
	printf "tools: %d\n", total
	printf "derived kind: read %d, write %d (exec is not derivable: no hint)\n", kinds["read"], kinds["write"]
	print ""
	print "declared -> derived tier (count):"
	for (p in pair) printf "  %-40s %d\n", p, pair[p]
	print ""
	agree = total - nd
	printf "agree: %d, differ: %d\n", agree, nd
	for (r in reasons) printf "  %3d  %s\n", reasons[r], r
	print ""
	print "differences:"
	for (i = 1; i <= nd; i++) print "  " diff[i]
}
