toolbox-digest(1)

# NAME

toolbox-digest - markl digest ids
# SYNOPSIS

*digest* [*-a* _algorithm_] [_file_...]

# DESCRIPTION

One "_markl-id_  _name_" line per _file_ (a path, a *madder://blobs/* URI, or
*-*), or for stdin when none is given. The id is the digest in markl's
encoding (*markl-id*(7)), e.g. *sha256-...* or *blake2b256-...*.

The *blake2b256* id of a blob equals the digest in its *madder://blobs/*
URI, so *digest -a blake2b256 madder://blobs/*_id_ printing _id_ back
confirms the blob's content.

Runs inside toolbox itself, not as a separate program.

# FLAGS

*-a*, *--algorithm* _algorithm_
	*sha256* (default) or *blake2b256*: the markl digest formats.

# SEE ALSO

*toolbox*(1), *toolbox-sha256sum*(1), *toolbox-b2sum*(1), *markl-id*(7)
