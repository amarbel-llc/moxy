toolbox-sort(1)

# NAME

toolbox-sort - sort lines
# SYNOPSIS

*sort* [_flag_...] [_file_...]

# DESCRIPTION

GNU *sort*(1) over the lines of every _file_ (a path, a *madder://blobs/*
URI, or *-*) together, or of stdin when none is given, under LC_ALL=C.UTF-8
(code point order).

# FLAGS

*-b*, *--ignore-leading-blanks*; *-d*, *--dictionary-order*;
*-f*, *--ignore-case*; *-g*, *--general-numeric-sort*;
*-h*, *--human-numeric-sort*; *-i*, *--ignore-nonprinting*;
*-M*, *--month-sort*; *-n*, *--numeric-sort*; *-V*, *--version-sort*;
*-r*, *--reverse*; *-s*, *--stable*; *-u*, *--unique*;
*-m*, *--merge*; *-c*, *--check*; *-C*;
*-z*, *--zero-terminated*
	as in *sort*(1).

*-k*, *--key* _keydef_
	sort by a key, e.g. *-k 2,2n*.

*-t*, *--field-separator* _char_
	field separator for *-k*.

Not available: *-o*/*--output* (writes a file), *-T*/*--temporary-directory*,
*--compress-program* (runs a program), *--files0-from* and *--random-source*
(read files the permission gate never judged), *-R* (not a pure function of
the input), *-S* and *--parallel*.

# SEE ALSO

*toolbox*(1)
