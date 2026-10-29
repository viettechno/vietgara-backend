-- Release 10.3 (FR-IAM-06): unlinking a Google account turns Google sign-in off
-- for the user until it links one again. Without it, a Google account with the
-- user's own verified e-mail would be linked again by its next sign-in, and
-- unlinking a compromised Google account would change nothing.

ALTER TABLE ONLY public.users
    ADD COLUMN google_signin_blocked boolean DEFAULT false NOT NULL;
