CREATE OR REPLACE TABLE `vpa-ensemble.borg.{cell}_events` AS
SELECT collection_id, instance_index, time, type, resource_request.memory AS req_mem
FROM `google.com:google-cluster-data`.clusterdata_2019_{cell}.instance_events
WHERE collection_id IN (SELECT collection_id FROM `vpa-ensemble.borg.{cell}_collections`);
