CREATE OR REPLACE TABLE `vpa-ensemble.borg.a_events` AS
SELECT collection_id, instance_index, time, type, resource_request.memory AS req_mem
FROM `google.com:google-cluster-data`.clusterdata_2019_a.instance_events
WHERE collection_id IN (SELECT collection_id FROM `vpa-ensemble.borg.a_collections`);
