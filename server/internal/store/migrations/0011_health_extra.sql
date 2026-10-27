-- Stage 10.5: resting heart rate, steps and workout minutes join the daily
-- health summary. Still only sums and means.
ALTER TABLE health_daily ADD COLUMN resting_hr REAL;
ALTER TABLE health_daily ADD COLUMN steps INTEGER;
ALTER TABLE health_daily ADD COLUMN workout_min INTEGER;
