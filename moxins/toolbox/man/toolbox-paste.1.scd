toolbox-paste(1)

# NAME

toolbox-paste - merge lines of inputs
# SYNOPSIS

*paste* [*-d* _list_] [*-s*] [*-z*] [_file_...]

# DESCRIPTION

GNU *paste*(1): the corresponding lines of each _file_ (a path, a
*madder://blobs/* URI, or *-* for stdin) side by side, or stdin alone when
none is given.

# FLAGS

*-d*, *--delimiters* _list_
	use the characters of _list_ in turn instead of tab.

*-s*, *--serial*
	join all lines of each input into one line.

*-z*, *--zero-terminated*
	lines end with NUL.

# SEE ALSO

*toolbox*(1)
