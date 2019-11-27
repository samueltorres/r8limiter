CREATE TABLE if not exists svc_ratelimiter.counters (
	key text,
	value counter,
	PRIMARY KEY (key)
)
WITH gc_grace_seconds = 172800;