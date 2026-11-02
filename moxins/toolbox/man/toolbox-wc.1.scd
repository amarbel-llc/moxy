toolbox-wc(1)

# NAME

toolbox-wc - count lines, words, characters and bytes
# SYNOPSIS

*wc* [*-c*] [*-m*] [*-l*] [*-w*] [*-L*] [_file_...]

# DESCRIPTION

GNU *wc*(1) over each _file_ (a path, a *madder://blobs/* URI, or *-*), or
stdin when none is given. Characters are counted as UTF-8.

*wc -c* _file_ is the byte size of _file_.

# FLAGS

*-c*, *--bytes*
	print the byte count.

*-m*, *--chars*
	print the character count.

*-l*, *--lines*
	print the newline count.

*-w*, *--words*
	print the word count.

*-L*, *--max-line-length*
	print the longest line's width.

*--files0-from* is not available: it reads a file the permission gate never
judged.

# SEE ALSO

*toolbox*(1)
