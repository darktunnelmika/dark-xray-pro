CREATE TABLE restore_subscriptions(
  id TEXT PRIMARY KEY,public_token TEXT UNIQUE NOT NULL,legacy_url TEXT NOT NULL,
  legacy_host TEXT NOT NULL,legacy_path TEXT NOT NULL,legacy_query TEXT NOT NULL,
  core_email TEXT NOT NULL,inbound_ids TEXT NOT NULL,node_ids TEXT NOT NULL,
  legacy_upload INTEGER NOT NULL,legacy_download INTEGER NOT NULL,
  legacy_total INTEGER NOT NULL,legacy_expire INTEGER NOT NULL,
  scan_status TEXT NOT NULL,enabled INTEGER NOT NULL,
  first_seen REAL NOT NULL,last_seen REAL NOT NULL,
  group_id TEXT NOT NULL,promoted_at REAL NOT NULL,promoted_owner TEXT NOT NULL,
  deleted_at REAL NOT NULL,deleted_reason TEXT NOT NULL,
  node_mode TEXT NOT NULL,include_local INTEGER NOT NULL
 );
 CREATE TABLE restore_groups(id TEXT PRIMARY KEY,name TEXT,name_key TEXT);
 CREATE TABLE restore_usage(restore_id TEXT,scope TEXT,up INTEGER,down INTEGER,PRIMARY KEY(restore_id,scope));
 CREATE TABLE restore_events(id INTEGER PRIMARY KEY AUTOINCREMENT, restore_id TEXT NOT NULL,event TEXT NOT NULL,detail TEXT NOT NULL DEFAULT '',at REAL NOT NULL);
 CREATE TABLE restore_domains(domain TEXT);
 CREATE TABLE restore_safety(restore_id TEXT PRIMARY KEY,metadata_state TEXT,external_disabled INTEGER NOT NULL DEFAULT 0,expected_enable INTEGER NOT NULL DEFAULT 1);
 CREATE TABLE restore_group_targets(group_id TEXT PRIMARY KEY,body TEXT);
 CREATE TABLE core_clients(email TEXT PRIMARY KEY);
ALTER TABLE core_clients ADD COLUMN body TEXT;
INSERT INTO restore_groups VALUES('synthetic-group','Synthetic source','synthetic-source');
INSERT INTO restore_group_targets VALUES('synthetic-group','{}');
INSERT INTO core_clients VALUES('synthetic-core','{"id":"01234567-89ab-4cde-8fab-0123456789ab"}');
INSERT INTO restore_subscriptions VALUES('synthetic-source','synthetic-token','https://example.invalid/sub','example.invalid','/sub','','synthetic-core','[1]','[]',100,200,5000,1893456000,'verified',1,0,0,'synthetic-group',0,'',0,'','selected',1);
INSERT INTO restore_safety(restore_id,metadata_state) VALUES('synthetic-source','verified');
INSERT INTO restore_usage VALUES('synthetic-source','local',3,7);
INSERT INTO restore_events VALUES(9,'synthetic-source','group.changed','{"from":"synthetic-old-group","to":"synthetic-group"}',1720000000.125);
