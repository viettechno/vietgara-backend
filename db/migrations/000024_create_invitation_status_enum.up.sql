CREATE TYPE public.invitation_status AS ENUM (
    'pending',
    'accepted',
    'declined',
    'revoked',
    'expired'
);
