# r8limiter

## Overview
r8limiter is an Envoy pluggable RateLimit gRPC service. It can serve multiple purposes, e.g - API Gateways, Database accesses, it is totally configurable in terms rate limiting rules, which offer a very big range of use cases.
This service uses the Sliding Window rate limiting algorithm, with asynchronous data replication. 

## Rules Configuration
All rate limiting rules are being described in yaml format.Please look at the following example:

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

In this pretty simple example we are limiting an authenticated user to 10000 requests per hour, and all the other users can do 500 per hour.

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

## Datastores

This rate limiter functions primarily in-memory, but as there can be multiple instances across clusters, it needs a centralized data-store in order to have all instances with the same amount of requests for a given request descriptor.

Currently it supports Redis and Cassandra.