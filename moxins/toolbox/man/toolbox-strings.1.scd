toolbox-strings(1)

# NAME

toolbox-strings - printable character runs in binary inputs

# SYNOPSIS

*strings* [*-n* _length_] [_file_...]

# DESCRIPTION

Each run of at least _length_ printable characters in each _file_ (a path,
a *madder://blobs/* URI, or *-*), or in stdin when none is given, one per
line, in the order they occur. Printable means ASCII from space to *~*,
and tab. Unlike GNU *strings*(1) it reads every byte of the input, not
only an object file's data sections, and prints no file names or offsets.

Pipe it into *string match* to check a build for a string without a shell:

	strings result/bin/tool | string match -re 'NEW_FLAG|patched'

Runs inside toolbox itself, not as a separate program.

# FLAGS

*-n*, *--bytes* _length_
	The shortest run printed, a positive integer (default 4).

# SEE ALSO

*toolbox*(1), *toolbox-string*(1)
