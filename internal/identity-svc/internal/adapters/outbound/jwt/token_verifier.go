package jwt

import commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"

type TokenVerifier = commonjwt.TokenVerifier

var NewTokenVerifier = commonjwt.NewTokenVerifier
