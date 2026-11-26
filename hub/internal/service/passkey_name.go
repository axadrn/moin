package service

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxPasskeyNameLength is the most characters a passkey's name has.
const maxPasskeyNameLength = 64

// defaultPasskeyName names a passkey whose password manager is unknown.
const defaultPasskeyName = "Passkey"

// passkeyMakers names the password managers by the AAGUID their passkeys
// carry, for the name a new passkey gets. Only well-known ones: the
// person renames any passkey. Each AAGUID is the one its maker uses; we
// keep this short list by hand. The community list at
// github.com/passkeydeveloper/passkey-authenticator-aaguids has no
// license and empties in January 2027, so it is not copied here. Add a
// maker when its AAGUID is public. An AAGUID is a public ID, no secret:
// gitleaks:allow marks the lines gitleaks takes for a key.
var passkeyMakers = map[string]string{
	"bada5566-a7aa-401f-bd96-45619a55120d": "1Password",
	"fbfc3007-154e-4ecc-8c0b-6e020557d7bd": "Apple Passwords",         // gitleaks:allow
	"dd4ec289-e01d-41c9-bb89-70fa845d4bf2": "Apple Passwords",         // gitleaks:allow
	"ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4": "Google Password Manager", // gitleaks:allow
	"adce0002-35bc-c60a-648b-0b25f1f05503": "Chrome on Mac",           // gitleaks:allow
	"08987058-cadc-4b81-b6e1-30de50dcbe96": "Windows Hello",
	"9ddd1817-af5a-4672-a2b9-3e3dd95000a9": "Windows Hello",
	"6028b017-b1d4-4c02-b4b3-afcdafc96bb2": "Windows Hello",
	"d3452668-01fd-4c12-926c-83a4204853aa": "Microsoft Password Manager",
	"d548826e-79b4-db40-a3d8-11116f7e8349": "Bitwarden", // gitleaks:allow
	"531126d6-e717-415c-9320-3d9aa6981239": "Dashlane",
	"50726f74-6f6e-5061-7373-50726f746f6e": "Proton Pass",
	"b84e4048-15dc-4dd0-8640-f4f60813c8af": "NordPass",
	"0ea242b4-43c4-4a1b-8b17-dd6d0b6baec6": "Keeper",
	"b78a0a55-6ef8-d246-a042-ba0f6d55050c": "LastPass",
	"f3809540-7f14-49c1-a8b3-8f813b225541": "Enpass",
	"fdb141b2-5d84-443e-8a35-4698c205a502": "KeePassXC",
	"53414d53-554e-4700-0000-000000000000": "Samsung Pass",
}

// passkeyMaker is the name a new passkey gets: its password manager's,
// from the AAGUID it was made with, or "Passkey". Some passkeys send no
// AAGUID or only zeros.
func passkeyMaker(aaguid []byte) string {
	id, err := uuid.FromBytes(aaguid)
	if err != nil {
		return defaultPasskeyName
	}
	if name, ok := passkeyMakers[id.String()]; ok {
		return name
	}
	return defaultPasskeyName
}

// passkeyName is name trimmed, or ErrPasskeyName when it is empty, longer
// than 64 characters or holds a control character.
func passkeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxPasskeyNameLength || strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrPasskeyName
	}
	return name, nil
}
