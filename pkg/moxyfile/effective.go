package moxyfile

import (
	"fmt"
	"sort"
	"strings"
)

// RenderEffective writes cfg as a canonical moxyfile: fixed key order, sorted
// lists and tables, no `include`, and every command in array form (the string
// form re-splits on whitespace, so it cannot carry an argument containing a
// space). The same config always renders to the same bytes, and the output
// parses back to an equal config.
//
// Feed it a config loaded with one of the *Literal loaders: it writes values
// as they are, so a config loaded with header expansion would put the expanded
// secrets on disk.
func RenderEffective(cfg Config) []byte {
	var b strings.Builder

	writeBool(&b, "ephemeral", cfg.Ephemeral)
	writeBool(&b, "progressive-disclosure", cfg.ProgressiveDisclosure)
	writeBool(&b, "builtin-native", cfg.BuiltinNative)
	writeSortedSet(&b, "disable-moxins", cfg.DisableMoxins)
	writeSortedSet(&b, "disable-servers", cfg.DisableServers)
	// An empty allow-tools list is not the same as none: it allows nothing.
	if cfg.AllowTools != nil {
		fmt.Fprintf(&b, "allow-tools = %s\n", tomlStringArray(sortedUnique(cfg.AllowTools)))
	}
	// An empty pass-tools list holds nothing, as an absent one does.
	writeSortedSet(&b, "pass-tools", cfg.PassTools)
	// Unset is delegate; writing it would turn "said nothing" into "said
	// delegate", which narrowing and the merge treat alike but a reader may not.
	if cfg.PermissionMode != "" {
		writeString(&b, "permission-mode", cfg.PermissionMode)
	}
	writeSortedSet(&b, "enforce-allow", cfg.EnforceAllow)

	if c := cfg.Credentials; c != nil {
		b.WriteString("\n[credentials]\n")
		writeString(&b, "read", c.Read)
		writeString(&b, "write", c.Write)
		writeString(&b, "delete", c.Delete)
	}

	// An empty record is not the same as none: it says discovery found no
	// moxins, which a child must then match.
	if cfg.ResolvedMoxins != nil {
		b.WriteString("\n[resolved-moxins]\n")
		writeSortedStringTable(&b, cfg.ResolvedMoxins)
	}

	servers := append([]ServerConfig(nil), cfg.Servers...)
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	for _, srv := range servers {
		renderServer(&b, srv)
	}

	return []byte(strings.TrimLeft(b.String(), "\n"))
}

func renderServer(b *strings.Builder, srv ServerConfig) {
	b.WriteString("\n[[servers]]\n")
	writeString(b, "name", srv.Name)
	if !srv.Command.IsEmpty() {
		fmt.Fprintf(b, "command = %s\n", tomlStringArray(srv.Command.Parts()))
	}
	if srv.URL != "" {
		writeString(b, "url", srv.URL)
	}
	if srv.HeadersHelper != nil {
		writeString(b, "headers-helper", *srv.HeadersHelper)
	}
	if srv.NixDevshell != nil {
		writeString(b, "nix-devshell", *srv.NixDevshell)
	}
	if srv.Paginate {
		b.WriteString("paginate = true\n")
	}
	writeBool(b, "generate-resource-tools", srv.GenerateResourceTools)
	writeBool(b, "ephemeral", srv.Ephemeral)
	writeBool(b, "progressive-disclosure", srv.ProgressiveDisclosure)

	if len(srv.Headers) > 0 {
		b.WriteString("\n[servers.headers]\n")
		writeSortedStringTable(b, srv.Headers)
	}
	if o := srv.OAuth; o != nil {
		b.WriteString("\n[servers.oauth]\n")
		writeString(b, "client-id", o.ClientID)
		fmt.Fprintf(b, "callback-port = %d\n", o.CallbackPort)
	}
	if a := srv.Annotations; a != nil {
		b.WriteString("\n[servers.annotations]\n")
		writeBool(b, "readOnlyHint", a.ReadOnlyHint)
		writeBool(b, "destructiveHint", a.DestructiveHint)
		writeBool(b, "idempotentHint", a.IdempotentHint)
		writeBool(b, "openWorldHint", a.OpenWorldHint)
	}
}

func writeSortedStringTable(b *strings.Builder, table map[string]string) {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s = %s\n", tomlString(k), tomlString(table[k]))
	}
}

func writeBool(b *strings.Builder, key string, v *bool) {
	if v != nil {
		fmt.Fprintf(b, "%s = %t\n", key, *v)
	}
}

func writeString(b *strings.Builder, key, v string) {
	fmt.Fprintf(b, "%s = %s\n", key, tomlString(v))
}

func writeSortedSet(b *strings.Builder, key string, entries []string) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "%s = %s\n", key, tomlStringArray(sortedUnique(entries)))
}

func sortedUnique(entries []string) []string {
	out := append([]string(nil), entries...)
	sort.Strings(out)
	n := 0
	for i, e := range out {
		if i == 0 || e != out[i-1] {
			out[n] = e
			n++
		}
	}
	return out[:n]
}

func tomlStringArray(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = tomlString(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// tomlString quotes s as a TOML basic string. strconv.Quote is not usable
// here: it emits \x and \a escapes that TOML rejects.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
