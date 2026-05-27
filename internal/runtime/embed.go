package runtime

import _ "embed"

//go:embed src/soyuz.h
var SoyuzHeader []byte

//go:embed src/rc.c
var Source []byte

//go:embed src/std_io.c
var StdIOSource []byte

//go:embed src/std_string.c
var StdStringSource []byte

//go:embed src/std_fs.c
var StdFSSource []byte

//go:embed src/std_os.c
var StdOSSource []byte

//go:embed src/std_collections.c
var StdCollectionsSource []byte

//go:embed src/soyuz_rt.h
var SoyuzRTHeader []byte

//go:embed src/soyuz_rt.c
var SoyuzRTSource []byte

//go:embed src/std_sync.c
var StdSyncSource []byte

//go:embed src/std_channel.c
var StdChannelSource []byte

//go:embed src/std_arc.c
var StdArcSource []byte
