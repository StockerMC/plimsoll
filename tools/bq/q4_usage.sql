-- Billed for the whole table's seven columns regardless of the WHERE; no alloc filter because the sample is top level already.
CREATE OR REPLACE TABLE `vpa-ensemble.borg.a_usage` AS
SELECT collection_id, instance_index, start_time, end_time,
       average_usage.memory AS avg_mem,
       maximum_usage.memory AS max_mem,
       assigned_memory
FROM `google.com:google-cluster-data`.clusterdata_2019_a.instance_usage
WHERE collection_id IN (SELECT collection_id FROM `vpa-ensemble.borg.a_sample`);
