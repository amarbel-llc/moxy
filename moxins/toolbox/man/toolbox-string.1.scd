toolbox-string(1)

# NAME

toolbox-string - fish-style string operations, in process
# SYNOPSIS

*string* _subcommand_ [_flag_...] [_argument_...] [_string_...]

# DESCRIPTION

Modelled on fish's *string* builtin and run inside toolbox, with no program,
file, network or environment access. The _strings_ are operands, and they are
values, not paths, as with *tr*'s sets: nothing is opened, so the permission
gate has nothing to judge. With no _string_ operands, each line of the
input (stdin, or the previous stage's output) is one string, and the final
newline is dropped. Output is one line per result, so stages chain.

Lengths and indexes count Unicode code points. A combining character is one
of its own, so *e* followed by U+0301 has length 2. NUL and control
characters are ordinary characters.

Regular expressions are Go's RE2 syntax. Matching takes time linear in the
input, so a pattern such as *'(a+)+$'* cannot backtrack exponentially. A pattern
that does not compile is refused when the command is parsed.

Each stage reads at most 16 MiB and writes at most 16 MiB. A stage that
would go past either limit fails, and its partial output is discarded.

Put *--* before a string that starts with *-*. Quote anything holding a
character toolbox refuses unquoted (see *toolbox*(1) GRAMMAR).

# SUBCOMMANDS

*length*
	the length of each string.

*sub* [*-s* _start_] [*-l* _length_ | *-e* _end_]
	a substring. _start_ is 1-based and counts from the end when negative
	(*-s -2* is the last two). *-l* takes a length; *-e* takes an inclusive
	end, also negative from the end. Indexes past either end are clamped.

*upper*, *lower*
	case-convert each string.

*trim* [*-l*] [*-r*] [*-c* _chars_]
	strip whitespace, or the characters in _chars_, from both ends. *-l*
	strips the left end only and *-r* the right end only.

*split* [*-m* _max_] [*-r*] [*-n*] _separator_
	split each string at a literal _separator_, one piece per line. An empty
	_separator_ splits into characters. *-m* makes at most _max_ splits,
	counted from the right with *-r*. *-n* drops empty pieces.

*join* _separator_
	join all the strings into one line.

*replace* [*-a*] [*-r*] [*-f*] _pattern_ _replacement_
	replace the first match, or every match with *-a*. _pattern_ is literal,
	or a regular expression with *-r*, in which case _replacement_ may
	name groups as *$1* or *${name}* (quoted, since *$* is refused
	unquoted). *-f* prints only the strings that changed.

*match* [*-r*] [*-a*] [*-e*] [*-v*] [*-g*] _pattern_
	print the strings matching _pattern_. Without *-r*, _pattern_ is a glob
	over the whole string, where *\** is any run of characters and *?* is
	one character. With *-r*, it prints each match followed by its capture
	groups, one per line. *-a* prints every match, *-e* prints the whole
	string instead, *-g* prints the groups without the match, and *-v*
	prints the strings that do not match.

*pad* [*-w* _width_] [*-c* _char_] [*-r*]
	pad each string on the left to _width_ characters with _char_ (default a
	space), or on the right with *-r*. A string already as wide is left as
	it is.

*repeat* [*-n* _count_] [*-m* _max_] [*-N*]
	repeat each string _count_ times, stopping at _max_ characters. At least
	one of *-n* and *-m* is required. *-N* omits the newline.

*collapse*
	replace each run of whitespace with one space.

*escape*, *unescape* [*--style* _shell_|_url_|_json_] [*--quotes*]
	quote each string for a context, or undo it. The style is *shell* by
	default.

	*shell* escape single-quotes the string so that it is one sh word (a
	quote inside is closed, escaped and reopened: *it'\\''s*). Unescape
	reads one sh word back: single quotes, double quotes (with *\\"*,
	*\\\\*, *\\$* and *\\`* escaped) and backslashes, and nothing is
	evaluated. Unquoted whitespace would make two words and is an error.

	*url* escape percent-encodes every byte except the unreserved ones
	(letters, digits, and the four characters hyphen, period, underscore
	and tilde), as upper-case *%XX*, so any bytes
	encode, UTF-8 or not. Unescape decodes *%XX* (either case) and leaves
	*+* alone; a *%* not followed by two hex digits is an error. The
	decoded bytes are whatever was encoded, a newline included.

	*json* escape writes a JSON string literal's body (*\\t*, *\\u0000*,
	*\\"*), or the literal with its quotes under *--quotes*. Input that is
	not valid UTF-8 is an error, since a JSON string cannot hold it.
	Unescape reads a body back, or a quoted literal under *--quotes*; an
	invalid escape is an error.

	*--quotes* applies to *--style=json* only.

# EXAMPLES

The length of a manpage NAME line:

	string length 'toolbox-wc - count lines, words, characters and bytes'

Split each line at its first comma, upper-cased:

	string split -m 1 , | string upper   (with stdin set)

Hosts from user@host addresses:

	string replace -r '(\\w+)@(\\w+)' '$2' alice@example

# NOT YET

JSON output (moxy#506) is not implemented.

# SEE ALSO

*toolbox*(1), *toolbox-tr*(1)
