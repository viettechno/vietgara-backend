-- Row audit (NFR-MT-07): fill created_by / updated_by from app.current_user_id
-- (NULL for a system job). created_at / created_by never change.
CREATE FUNCTION public.set_row_audit() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.created_by := COALESCE(app_current_user_id(), NEW.created_by);
        NEW.updated_by := COALESCE(app_current_user_id(), NEW.updated_by, NEW.created_by);
    ELSE
        NEW.created_at := OLD.created_at;
        NEW.created_by := OLD.created_by;
        NEW.updated_at := NOW();
        NEW.updated_by := COALESCE(
            app_current_user_id(),
            CASE WHEN NEW.updated_by IS DISTINCT FROM OLD.updated_by THEN NEW.updated_by END);
    END IF;

    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

CREATE FUNCTION public.set_created_audit() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.created_by := COALESCE(app_current_user_id(), NEW.created_by);

    RETURN NEW;
END;
$$;
