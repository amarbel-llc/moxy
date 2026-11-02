toolbox-uniq(1)

# NAME

toolbox-uniq - collapse repeated adjacent lines
# SYNOPSIS

*uniq* [_flag_...] [_file_]

# DESCRIPTION

GNU *uniq*(1) over one _file_ (a path, a *madder://blobs/* URI, or *-*), or
stdin. Only adjacent lines are compared: sort first for global uniqueness.

At most one operand: *uniq*'s second operand is an output file, so it does
not exist here.

# FLAGS

*-c*, *--count*
	prefix lines with their number of occurrences.

*-d*, *--repeated*
	print only repeated lines, once each.

*-u*, *--unique*
	print only lines that do not repeat.

*-i*, *--ignore-case*
	compare ignoring case.

*-f*, *--skip-fields* _n_; *-s*, *--skip-chars* _n_; *-w*, *--check-chars* _n_
	skip or limit what is compared.

*-z*, *--zero-terminated*
	lines end with NUL.

# SEE ALSO

*toolbox*(1), *toolbox-sort*(1)
