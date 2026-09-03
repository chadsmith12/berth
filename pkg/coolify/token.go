package coolify

import "regexp"

var tokenRe = regexp.MustCompile(`^\d+\|\S+$`)

// ValidTokenFormat reports whether the token looks like "<id>|<secret>".
// The number before the pipe is the token id and both parts are required.
func ValidTokenFormat(token string) bool {
	return tokenRe.MatchString(token)
}
