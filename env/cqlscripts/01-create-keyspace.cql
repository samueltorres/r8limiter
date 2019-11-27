CREATE KEYSPACE IF NOT EXISTS svc_ratelimiter
  WITH REPLICATION = {
   'class' : 'NetworkTopologyStrategy',
   'datacenter1' : 1
  };