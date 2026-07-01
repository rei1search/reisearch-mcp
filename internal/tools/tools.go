package tools

import "context"

type contextKey string

const tokenContextKey contextKey = "token"

func WithToken(ctx context.Context, token string) context.Context {
	ctx = context.WithValue(ctx, tokenContextKey, token)
	return ctx
}

func TokenFromContext(ctx context.Context) string {
	value, _ := ctx.Value(tokenContextKey).(string)
	return value
}
