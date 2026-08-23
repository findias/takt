module github.com/konkov/agile

go 1.26.6

// Версия языка — она же нижняя граница тулчейна, и поднимается она
// не за новыми возможностями, а по разбору уязвимостей: govulncheck
// 23.08.2026 нашёл семь достижимых дыр в стандартной библиотеке
// 1.26.4 — net/url, net/http, crypto/tls, encoding/xml, encoding/asn1.
// Все закрыты в 1.26.6. Сборка на более старом тулчейне теперь
// откажется, и это правильнее, чем собраться молча.

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pb33f/libopenapi v0.38.7
	github.com/pb33f/libopenapi-validator v0.14.0
	go.yaml.in/yaml/v4 v4.0.0-rc.6
	golang.org/x/crypto v0.55.0
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/basgys/goxml2json v1.1.1-0.20231018121955-e66ee54ceaad // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/go-openapi/jsonpointer v0.23.2 // indirect
	github.com/go-openapi/swag/jsonname v0.26.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/pb33f/jsonpath v0.8.2 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
