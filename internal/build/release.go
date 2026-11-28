package build

// ReleaseKeys are the public Ed25519 keys whose signature a release must
// carry, base64: the key that signs every release and a spare kept
// offline. A server checks `moin update` with them, cli.sh on the site
// what it installs. The Deploy workflow checks that it signs with the
// first.
var ReleaseKeys = []string{
	"dkwXdu3Ecl8Rs4tfXxIrvipOfRa5R61tfWEdKISw3LA=", // RELEASE_SIGNING_KEY
	"K2CVHFrtBYK1IT7s++2/R/LZNxO6+jGPc68EZiUDKhQ=", // RELEASE_SIGNING_KEY_SPARE, offline
}
