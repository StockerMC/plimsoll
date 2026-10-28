CREATE OR REPLACE TABLE `vpa-ensemble.borg.a_sample` AS
SELECT c.*, e.n_instances, e.n_update_running, e.n_mem_changes
FROM `vpa-ensemble.borg.a_collections` c
JOIN (SELECT collection_id,
             COUNT(DISTINCT instance_index) AS n_instances,
             COUNTIF(type = 10) AS n_update_running,
             COUNTIF(changed) AS n_mem_changes
      FROM (SELECT collection_id, instance_index, type,
                   type = 10 AND req_mem != LAG(req_mem) OVER (PARTITION BY collection_id, instance_index ORDER BY time, type) AS changed
            FROM `vpa-ensemble.borg.a_events`)
      GROUP BY collection_id) e USING (collection_id)
WHERE e.n_instances BETWEEN 2 AND 50
QUALIFY ROW_NUMBER() OVER (PARTITION BY vertical_scaling
                           ORDER BY FARM_FINGERPRINT(CAST(collection_id AS STRING))) <= 200;
