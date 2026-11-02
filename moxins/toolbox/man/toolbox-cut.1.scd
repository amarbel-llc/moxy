toolbox-cut(1)

# NAME

toolbox-cut - select fields, characters or bytes of each line
# SYNOPSIS

*cut* (*-f* _list_ | *-c* _list_ | *-b* _list_) [*-d* _char_] [_flag_...] [_file_...]

# DESCRIPTION

GNU *cut*(1) over each _file_ (a path, a *madder://blobs/* URI, or *-*), or
stdin. Give exactly one of *-f*, *-c*, *-b*; a _list_ is like *1,3-4*.

# FLAGS

*-f*, *--fields* _list_; *-c*, *--characters* _list_; *-b*, *--bytes* _list_
	what to select.

*-d*, *--delimiter* _char_
	field delimiter for *-f* (default tab).

*-s*, *--only-delimited*
	skip lines without the delimiter.

*--complement*
	select everything but _list_.

*--output-delimiter* _string_
	join selected fields with _string_.

*-z*, *--zero-terminated*
	lines end with NUL.

# SEE ALSO

*toolbox*(1)
