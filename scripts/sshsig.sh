#!/bin/sh
# Signs a file like `ssh-keygen -Y sign -n moin-release` does, with an
# Ed25519 key in PEM on stdin, and writes the armored signature to stdout.
# cli.sh checks it with `ssh-keygen -Y verify`, which every Mac and Linux
# has. ssh-keygen itself reads no PEM Ed25519 key, so openssl signs, and
# this wraps the signature in the format of OpenSSH's PROTOCOL.sshsig.
# Every field has a fixed length, written as its 4-byte length first:
# \013 is 11 ("ssh-ed25519"), \014 12 ("moin-release"), \006 6 ("sha512"),
# \040 32 (the public key), \100 64 (the hash, the signature), \063 51 and
# \123 83 (the public key and the signature with their type).
#
# Usage: printf '%s\n' "$KEY" | sh scripts/sshsig.sh SHA256SUMS > SHA256SUMS.sshsig
set -eu
file=$1
key=$(cat)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$key" | openssl pkey -pubout -outform DER | tail -c 32 > "$tmp/public"

# What the key signs: the magic, the namespace, nothing reserved, the hash
# and its name.
{
	printf 'SSHSIG\000\000\000\014moin-release\000\000\000\000\000\000\000\006sha512\000\000\000\100'
	openssl dgst -sha512 -binary "$file"
} > "$tmp/signed"
printf '%s\n' "$key" | openssl pkeyutl -sign -rawin -inkey /dev/stdin -in "$tmp/signed" -out "$tmp/signature"

# The signature: the magic, version 1, the public key, the namespace,
# nothing reserved, the hash's name and the signature itself.
{
	printf 'SSHSIG\000\000\000\001'
	printf '\000\000\000\063\000\000\000\013ssh-ed25519\000\000\000\040'
	cat "$tmp/public"
	printf '\000\000\000\014moin-release\000\000\000\000\000\000\000\006sha512'
	printf '\000\000\000\123\000\000\000\013ssh-ed25519\000\000\000\100'
	cat "$tmp/signature"
} > "$tmp/blob"
echo "-----BEGIN SSH SIGNATURE-----"
openssl base64 -in "$tmp/blob"
echo "-----END SSH SIGNATURE-----"
