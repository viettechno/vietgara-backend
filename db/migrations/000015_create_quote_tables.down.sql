DROP FUNCTION IF EXISTS find_quote_by_public_token(TEXT);

DROP TABLE IF EXISTS quote_lines;
DROP TABLE IF EXISTS quotes;

DROP TYPE IF EXISTS quote_line_decision;
DROP TYPE IF EXISTS quote_item_type;
DROP TYPE IF EXISTS quote_status;
