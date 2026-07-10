-- Extends raw retention from 30 days to 13 months (SPEC.md §6 decision,
-- 2026-07-09). The frontends still only ever window on the current UTC
-- calendar week/month (usage_daily is a view, so it has no retention of
-- its own beyond `requests`'), but keeping ~13 months of raw rows around
-- means static analytics (month-over-month trends, a future yearly
-- view) can be built later without re-plumbing retention. 13 months
-- (not 12) keeps the original migration's one-month redundancy against
-- the monthly cron's own scheduling slack.
--
-- cron.schedule() upserts by job name, so re-registering
-- 'burnbar-prune-requests' here updates the existing job's command in
-- place rather than creating a second job.
select cron.schedule(
  'burnbar-prune-requests',
  '0 3 1 * *',
  $$ delete from public.requests
     where requested_at < now() - interval '13 months' $$
);
