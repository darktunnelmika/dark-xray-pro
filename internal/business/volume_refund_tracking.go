package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Prospective proof survives older writers: persistent SQL triggers capture
// identity creation and potential native exposure in the originating write.
// Exposure starts at binding, before preparation or issuance. It is never
// cleared by an unbind, target edit, stop, or lease timeout, so an old prepared
// candidate cannot disappear from this proof when current membership changes.
var volumeRefundTrackingSchema = func() []string {
	ddl := []string{
		`CREATE TABLE volume_refund_created_identities(client_id TEXT PRIMARY KEY REFERENCES clients(id),client_uuid TEXT NOT NULL,created_unix INTEGER NOT NULL)`,
		`CREATE TABLE volume_refund_native_exposures(client_id TEXT PRIMARY KEY REFERENCES clients(id),client_uuid TEXT NOT NULL,inbound_id INTEGER NOT NULL,source_kind TEXT NOT NULL CHECK(source_kind IN('binding','target','native','metered_route')),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,recorded_unix INTEGER NOT NULL)`,
		`CREATE TRIGGER volume_refund_track_identity AFTER INSERT ON clients BEGIN INSERT INTO volume_refund_created_identities(client_id,client_uuid,created_unix) VALUES(NEW.id,NEW.uuid,NEW.created_unix); END`,
	}
	for _, table := range []string{"volume_refund_created_identities", "volume_refund_native_exposures"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			ddl = append(ddl, fmt.Sprintf("CREATE TRIGGER %s_no_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'volume refund exposure proof immutable'); END", table, map[string]string{"UPDATE": "update", "DELETE": "delete"}[action], action, table))
		}
	}
	for _, event := range []struct{ name, sql string }{{"insert", "INSERT"}, {"update", "UPDATE OF inbound_id,client_id"}} {
		ddl = append(ddl, fmt.Sprintf(`CREATE TRIGGER volume_refund_track_binding_%s AFTER %s ON native_inbound_clients BEGIN
 INSERT OR IGNORE INTO volume_refund_native_exposures(client_id,client_uuid,inbound_id,source_kind,node_id,installation_id,recorded_unix)
 SELECT id,uuid,NEW.inbound_id,'binding','','',CAST(strftime('%%s','now') AS INTEGER) FROM clients WHERE id=NEW.client_id; END`, event.name, event.sql))
	}
	for _, event := range []struct{ name, sql string }{{"insert", "INSERT"}, {"update", "UPDATE"}} {
		ddl = append(ddl, fmt.Sprintf(`CREATE TRIGGER volume_refund_track_target_%s AFTER %s ON native_inbound_targets BEGIN
 INSERT OR IGNORE INTO volume_refund_native_exposures(client_id,client_uuid,inbound_id,source_kind,node_id,installation_id,recorded_unix)
 SELECT c.id,c.uuid,NEW.inbound_id,'target',NEW.node_id,NEW.installation_id,CAST(strftime('%%s','now') AS INTEGER)
 FROM native_inbound_clients b JOIN clients c ON c.id=b.client_id WHERE b.inbound_id=NEW.inbound_id; END`, event.name, event.sql))
		for _, source := range []struct{ name, table string }{{"native", "native_runtime_delivery"}, {"metered_route", "native_metered_route_commands"}} {
			ddl = append(ddl, fmt.Sprintf(`CREATE TRIGGER volume_refund_track_%s_%s AFTER %s ON %s BEGIN
 INSERT OR IGNORE INTO volume_refund_native_exposures(client_id,client_uuid,inbound_id,source_kind,node_id,installation_id,recorded_unix)
 SELECT c.id,c.uuid,b.inbound_id,'%s',NEW.node_id,NEW.installation_id,CAST(strftime('%%s','now') AS INTEGER)
 FROM native_inbound_targets t JOIN native_inbound_clients b ON b.inbound_id=t.inbound_id JOIN clients c ON c.id=b.client_id
 WHERE t.node_id=NEW.node_id AND t.installation_id=NEW.installation_id; END`, source.name, event.name, event.sql, source.table, source.name))
		}
	}
	return ddl
}()

func volumeRefundTrackedUnexposed(ctx context.Context, tx *sql.Tx, c Client) (bool, error) {
	var uuid string
	var created int64
	err := tx.QueryRowContext(ctx, `SELECT client_uuid,created_unix FROM volume_refund_created_identities WHERE client_id=?`, c.ID).Scan(&uuid, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if uuid != c.UUID || created != c.CreatedUnix {
		return false, nil
	}
	var exposed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM volume_refund_native_exposures WHERE client_id=?)
 OR EXISTS(SELECT 1 FROM native_inbound_clients WHERE client_id=?)`, c.ID, c.ID).Scan(&exposed)
	return !exposed, err
}
