module github.com/shrtyk/e-commerce-platform/internal/identity-svc

go 1.26.0

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/go-chi/chi/v5 v5.2.5
	github.com/jackc/pgx/v5 v5.9.1
	github.com/oapi-codegen/runtime v1.3.1
	github.com/shrtyk/e-commerce-platform/internal/common v0.0.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/BurntSushi/toml v1.3.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/ilyakaznacheev/cleanenv v1.5.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	olympos.io/encoding/edn v0.0.0-20201019073823-d3554ca0b0a3 // indirect
)

replace github.com/shrtyk/e-commerce-platform/internal/common => ../common
