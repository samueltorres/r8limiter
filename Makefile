run:
	go run cmd/server/server.go

docker-build:
	docker build -t localhost:32000/service-r8limiter:0.1 .

docker-push:
	docker push localhost:32000/service-r8limiter:0.1