package i18n

import "github.com/gin-gonic/gin"

// Error is an error whose message is a translation key. Error() renders the
// default language for logs and tests; common.ApiError renders the caller's
// language through LocalizedMessage.
type Error struct {
	Key    string
	Params map[string]any
}

// NewError returns an error that is translated when it reaches the caller.
func NewError(key string, params map[string]any) error {
	return &Error{Key: key, Params: params}
}

func (e *Error) Error() string {
	return Translate(DefaultLang, e.Key, e.Params)
}

// LocalizedMessage renders the message in the language of the request.
func (e *Error) LocalizedMessage(c *gin.Context) string {
	return T(c, e.Key, e.Params)
}
