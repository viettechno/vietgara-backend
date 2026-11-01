-- Search that ignores diacritics: "nguyen van anh" finds "Nguyễn Văn Ánh" (customers, vehicles, suppliers, parts, jobs).
CREATE EXTENSION IF NOT EXISTS unaccent;
