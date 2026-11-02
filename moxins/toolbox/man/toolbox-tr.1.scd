toolbox-tr(1)

# NAME

toolbox-tr - translate, squeeze or delete characters
# SYNOPSIS

*tr* [*-c*] [*-d*] [*-s*] [*-t*] [*--*] _set1_ [_set2_]

# DESCRIPTION

GNU *tr*(1) from stdin to stdout. _set1_ and _set2_ are values, not files:
character lists, ranges (*a-z*) and classes (*'[:upper:]'*, quoted, since
*[* is refused unquoted). Put *--* before a set that starts with *-*.

# FLAGS

*-c*, *-C*, *--complement*
	use the complement of _set1_.

*-d*, *--delete*
	delete characters in _set1_.

*-s*, *--squeeze-repeats*
	squeeze runs of a character in the last set to one.

*-t*, *--truncate-set1*
	truncate _set1_ to the length of _set2_.

# SEE ALSO

*toolbox*(1)
