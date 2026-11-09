package fixture

import "time"

// garageZone is the zone the platform counts days in (Asia/Ho_Chi_Minh, UTC+7,
// no daylight saving). A fixed offset keeps the helper independent of the
// time zone database of the machine that runs the suite.
var garageZone = time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)

// GarageDate is the date, daysAgo days before today, as the platform counts
// it (a day in Vietnam, not in UTC): scenarios that compare with the date the
// server stamped must not use time.Now() in the runner's own zone, which
// differs between 17:00 and 24:00 UTC.
func GarageDate(daysAgo int) string {
	return time.Now().In(garageZone).AddDate(0, 0, -daysAgo).Format(time.DateOnly)
}
