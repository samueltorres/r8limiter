# r8limiter

r8limiter is an Envoy pluggable RateLimit gRPC service. It can serve multiple purposes, e.g - API Gateways, Database accesses, Service-to-Service communications etc. It offers a totally configurable API, in which you can define the rules by which a request can be limited.
This service uses the sliding window rate limiting algorithm, with asynchronous or synchronous data replication, or even an in-mem rate limiter.

#### CI Status
[![CircleCI](https://circleci.com/gh/samueltorres/r8limiter.svg?style=svg)](https://circleci.com/gh/samueltorres/r8limiter)


# Installation
```
go get github.com/samueltorres/r8limiter
```

# Usage
```
Usage of r8limiter:
  -cassandra-host string
        cassandra host
  -cassandra-keyspace string
        cassandra keyspace
  -datastore string
        datastore type (redis/cassandra) (default "redis")
  -grpc-addr string
        grpc address (default ":8081")
  -http-addr string
        http address (default ":8082")
  -log-level string
        log level (panic, fatal, error, warn, info, debug, trace) (default "info")
  -redis-address string
        redis address
  -redis-database int
        redis database
  -redis-password string
        redis password
  -rules-file string
        rules file (default "env/rules.yaml")
```

# Rules Configuration
Rate limiting rules are being described in yaml format. 

Please look at the following example:

```yaml
domains:
  - domain: envoy
    rules:
      - name: authenticated users
        labels:
          - key: authenticated
            value: "true"
          - key: user_id
        limit:
          unit: hour
          requests: 500
        syncRate: 1

      - name: any user
        labels:
          - key: user_id
        limit:
          unit: hour
          requests: 1000
        syncRate: -1
```

In this pretty simple example we are limiting any authenticated user to make 500 requests per hour, and all the other users can do 1000 per hour. 

You're able to configure the synchronization rate for a given rule, as sometimes we cannot be lenient doing rate limiting (e.g - enterprise offerings), and we need to have a synchronized rate limiter. In other use cases we may even do rate limiting, just to protect the server from a DDOS attack, and an in-mem rate limit is sufficient. 

| sync rate |                                                       |
|-----------|-------------------------------------------------------|
| -1        | no synchronization                                    |
| 0         | all rate limit requests go through the remote storage |
| [1-n]     | number of seconds between synchronizations            |


This configuration schema allows you to configure different rate limits configurations for each kind use case you'll need, e.g - limiting mongodb accesses to 10000 reqs/hour, limiting Portuguese users to 20 reqs/min etc.

```yaml
domains:
  - domain: api-gateway
    rules:
      - name: any user
        labels:
          - key: country
            value: PT
          - key: user_id
        limit:
          unit: minute
          requests: 20
  - domain: mongodb
    rules:
      - name: any request
        labels:
          - key: generic
        limit:
          unit: hour
          requests: 10000
```

# Remote Storages
When synchronization is needed, a remote storage must be provided, currently it supports Redis and Cassandra.

# Run locally

Spin up environment using docker-compose

```bash
docker-compose up
```

Spin up the r8limiter server

```bash
go run cmd/server/server.go --rules-file ./env/rules.yaml
```

Spin up a test client
```bash
go run cmd/client/client.go
```

# License
This project is licensed under the MIT open source license.