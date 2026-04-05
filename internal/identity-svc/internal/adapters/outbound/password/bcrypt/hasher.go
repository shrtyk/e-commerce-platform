package bcrypt

import "golang.org/x/crypto/bcrypt"

type Hasher struct {
	cost int
}

// NewHasher create new hasher instance with configured cost value.
//
// If cost = 0 default bcrypt.DefaultCost will be used.
func NewHasher(cost int) *Hasher {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}

	return &Hasher{cost: cost}
}

func (h *Hasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}
