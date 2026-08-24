INSERT OR IGNORE INTO users(id,username,password_hash,display_name,role,active,created_at,updated_at) VALUES
('usr_operator','operator','sha256:8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918','悠宿运营','operator',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('usr_frontdesk','frontdesk','sha256:85262adf74518bbb70c7e55a1e0f23ccf008ede4a65115afb5f475b3b285db62','悠宿前台','frontdesk',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('usr_cleaner','cleaner','sha256:b1202940ff2c9f1137e40a35d2311e7a44a052aae352c529a4b9c23616a2902d','清洁值守','cleaner',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('usr_maintenance','maintenance','sha256:9d34876fb4ac8bd8ed73a0eb2cf0fb36fda938b3fac2b3fe1c63cc7d398c875f','维修值守','maintenance',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('usr_guard','guard','sha256:e6f01fc2405ba5b1acbdb78f43aa26727f5af09b7776ba90fef6e942368f7c17','营地值守','camp_guard',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);

