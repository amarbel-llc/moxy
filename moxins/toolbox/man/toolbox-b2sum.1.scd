toolbox-b2sum(1)

# NAME

toolbox-b2sum - BLAKE2b checksums
# SYNOPSIS

*b2sum* [*-l* _bits_] [*--tag*] [*-z*] [_file_...]

# DESCRIPTION

GNU *b2sum*(1): one "_hex_  _name_" line per _file_ (a path, a
*madder://blobs/* URI, or *-*), or for stdin when none is given.

*b2sum -l 256* is BLAKE2b-256, the digest madder names blobs by;
*toolbox-digest*(1) prints it as a markl id.

# FLAGS

*-l*, *--length* _bits_
	digest length, a multiple of 8 from 8 to 512 (default 512).

*--tag*
	BSD-style output.

*-z*, *--zero*
	end each line with NUL, not newline.

*-c*/*--check* is not available: it reads the files a checksum list names.

# SEE ALSO

*toolbox*(1), *toolbox-digest*(1)
