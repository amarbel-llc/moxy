toolbox-sha256sum(1)

# NAME

toolbox-sha256sum - SHA-256 checksums
# SYNOPSIS

*sha256sum* [*--tag*] [*-z*] [_file_...]

# DESCRIPTION

GNU *sha256sum*(1): one "_hex_  _name_" line per _file_ (a path, a
*madder://blobs/* URI, or *-*), or for stdin when none is given. A blob URI
is named */dev/fd/*_N_ in the output, the pipe moxy feeds it through.

# FLAGS

*--tag*
	BSD-style output.

*-z*, *--zero*
	end each line with NUL, not newline.

*-c*/*--check* is not available: it reads the files a checksum list names,
which the permission gate never judged. Compare the output yourself.

# SEE ALSO

*toolbox*(1), *toolbox-digest*(1)
