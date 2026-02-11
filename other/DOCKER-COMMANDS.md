docker-compose up -d
docker exec -it metapus-db psql -U metapus -c "CREATE DATABASE tenants;"
goose -dir db/migrations postgres "postgres://metapus:metapus@localhost:5432/metapus?sslmode=disable" up
goose -dir db/meta postgres "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable" up

docker rm metapus-db
docker volume rm metapus_postgres_data