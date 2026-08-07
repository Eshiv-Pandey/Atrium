-- Reverse of 000001_init.up.sql.
--
-- Tables drop in FK-dependency order. Extensions are left in place: they may
-- predate this migration or be shared with another schema in the same
-- database, so dropping them is not ours to do.

BEGIN;

DROP TABLE IF EXISTS bookings;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS users;

COMMIT;
