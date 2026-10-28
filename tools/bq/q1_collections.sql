DECLARE MAXT INT64 DEFAULT 9223372036854775807;
CREATE OR REPLACE TABLE `vpa-ensemble.borg.a_collections` AS
SELECT collection_id, priority, scheduling_class, vertical_scaling, user, logical_name
FROM (
  SELECT collection_id,
         LOGICAL_OR(time = 0) AS present_at_start,
         COUNTIF(type IN (4,5,6,7,8) AND time < MAXT) AS n_terminations,
         ANY_VALUE(collection_type) AS collection_type,
         MAX(IFNULL(alloc_collection_id, 0)) AS alloc_collection_id,
         MAX(priority) AS priority, MAX(scheduling_class) AS scheduling_class,
         MAX(vertical_scaling) AS vertical_scaling,
         ANY_VALUE(user) AS user, ANY_VALUE(collection_logical_name) AS logical_name
  FROM `google.com:google-cluster-data`.clusterdata_2019_a.collection_events
  GROUP BY collection_id)
WHERE present_at_start
  AND n_terminations = 0
  AND collection_type = 0 AND alloc_collection_id = 0
  AND priority BETWEEN 120 AND 359 AND scheduling_class >= 2
  AND vertical_scaling IN (1, 3);
