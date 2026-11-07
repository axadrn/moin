package kit

import (
	"fmt"
	"time"
)

// assetVersion changes with every start of the hub, so a
// browser loads the new CSS and JavaScript after an update.
var assetVersion = fmt.Sprint(time.Now().Unix())

// AssetURL is path with the version of this start, for the cache.
func AssetURL(path string) string {
	return path + "?v=" + assetVersion
}
