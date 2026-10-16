ALTER TABLE IF EXISTS repair_order_lines DROP CONSTRAINT IF EXISTS repair_order_lines_garage_unit_fkey;
ALTER TABLE IF EXISTS quote_lines DROP CONSTRAINT IF EXISTS quote_lines_garage_unit_fkey;

DROP TABLE IF EXISTS stock_transactions;
DROP TABLE IF EXISTS stock_balance;
DROP TABLE IF EXISTS item_images;
DROP TABLE IF EXISTS item_units;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS part_categories;
DROP TABLE IF EXISTS units;

DROP TYPE IF EXISTS stock_transaction_ref_type;
DROP TYPE IF EXISTS stock_transaction_type;
