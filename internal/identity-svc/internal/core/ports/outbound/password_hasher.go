package outbound

//mockery:generate: true
type PasswordHasher interface {
	Hash(password string) (string, error)
}
