package loadseed

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/seed"
)

var (
	errNoLoadSection  = errors.New("the dataset has no load section (needs vietgara-testdata v0.4.0 or newer)")
	errUnknownProfile = errors.New("unknown load profile")
	errNoAdmin        = errors.New("the dataset has no platform admin to approve the owners' plans")
	errGarageMissing  = errors.New("garage not found after seeding")
)

const (
	pageSize = 100
	// workers is how many garages are seeded at once. Seeding is write-heavy
	// and runs against a shared environment, so it stays modest.
	workers = 6
)

// Options says where and how to seed.
type Options struct {
	API           *client.Client
	Inbox         seed.Inbox
	Profile       string
	EmailDomain   string
	Password      string // the shared owner password
	AdminPassword string
	Log           func(format string, args ...any)
}

// Run creates (or finds, when it already exists) every owner, garage and record
// of the profile and returns the Population. It is safe to run twice: owners,
// garages, parts, jobs, customers and vehicles that exist are kept as they are.
func Run(ctx context.Context, dataset *testdata.Dataset, options Options) (*Population, error) {
	profile, err := profileByKey(dataset.Load, options.Profile)
	if err != nil {
		return nil, err
	}

	var admin *testdata.User

	for i := range dataset.Users {
		if dataset.Users[i].PlatformAdmin {
			admin = &dataset.Users[i]

			break
		}
	}

	if admin == nil {
		return nil, errNoAdmin
	}

	population := &Population{Profile: profile.Key, Owners: make([]Owner, profile.Garages)}

	// The first garage goes alone: it registers the platform admin when the
	// environment is fresh, which concurrent seeders would race to do.
	first, err := seedGarage(ctx, dataset, profile, *admin, options, 1)
	if err != nil {
		return nil, fmt.Errorf("garage 1: %w", err)
	}

	population.Owners[0] = *first

	options.Log("seeded garage 1 of %d", profile.Garages)

	if err := seedRest(ctx, dataset, profile, *admin, options, population); err != nil {
		return nil, err
	}

	return population, nil
}

// seedRest seeds garages 2 to profile.Garages with a few workers; the first
// failure stops the others.
func seedRest(
	ctx context.Context, dataset *testdata.Dataset, profile testdata.LoadProfile, admin testdata.User, options Options, population *Population,
) error {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	numbers := make(chan int)

	for range workers {
		wg.Go(func() {
			for n := range numbers {
				owner, err := seedGarage(ctx, dataset, profile, admin, options, n)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("garage %d: %w", n, err)

						cancel()
					}
					mu.Unlock()

					continue
				}

				mu.Lock()
				population.Owners[n-1] = *owner
				mu.Unlock()

				options.Log("seeded garage %d of %d", n, profile.Garages)
			}
		})
	}

feed:
	for n := 2; n <= profile.Garages; n++ {
		select {
		case numbers <- n:
		case <-ctx.Done():
			break feed
		}
	}

	close(numbers)
	wg.Wait()

	return firstErr
}

// seedGarage seeds the n-th owner, garage and everything in it.
func seedGarage(
	ctx context.Context, dataset *testdata.Dataset, profile testdata.LoadProfile, admin testdata.User, options Options, n int,
) (*Owner, error) {
	// The shared baseline seeder registers the owner, puts them on the plan
	// (an admin approves the payment) and creates the garage.
	own := &testdata.Dataset{
		Users: []testdata.User{
			admin,
			{Key: ownerKey(n), Name: ownerName(n), Phone: ownerPhone(n), Plan: profile.OwnerPlan},
		},
		Garages: []testdata.Garage{{
			Key: "loadgara" + strconv.Itoa(n), Owner: ownerKey(n), Name: garageName(n),
			Address: fmt.Sprintf("%d Le Loi, Quan 1, TP.HCM", n), Phone: fmt.Sprintf("028%07d", n), TaxCode: fmt.Sprintf("031%07d", n),
		}},
	}

	if _, err := seed.Run(ctx, own, seed.NewClientAPI(options.API), options.Inbox,
		options.EmailDomain, options.Password, options.AdminPassword); err != nil {
		return nil, fmt.Errorf("owner and garage: %w", err)
	}

	email := ownerKey(n) + "@" + options.EmailDomain

	session, err := options.API.Login(ctx, email, options.Password)
	if err != nil {
		return nil, fmt.Errorf("sign in: %w", err)
	}

	api := options.API.WithToken(session.GetAccessToken())

	garages, err := api.ListGarages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list garages: %w", err)
	}

	var garageID string

	for _, garage := range garages {
		if garage.GetName() == garageName(n) {
			garageID = garage.GetId()
		}
	}

	if garageID == "" {
		return nil, fmt.Errorf("%w: %q", errGarageMissing, garageName(n))
	}

	owner := &Owner{Email: email, GarageID: garageID}
	pool := dataset.Load

	if owner.Parts, err = seedParts(ctx, api, garageID, profile, pool); err != nil {
		return nil, fmt.Errorf("parts: %w", err)
	}

	if owner.Jobs, err = seedJobs(ctx, api, garageID, pool, owner.Parts); err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}

	if owner.Customers, err = seedCustomers(ctx, api, garageID, profile, pool, n); err != nil {
		return nil, fmt.Errorf("customers: %w", err)
	}

	return owner, nil
}

// seedParts creates the pool's parts under one base unit and receives the
// opening stock for every part it created. A part that already exists is
// assumed stocked.
func seedParts(ctx context.Context, api *client.Client, garageID string, profile testdata.LoadProfile, pool *testdata.LoadTest) ([]Part, error) {
	units, err := api.ListUnits(ctx, garageID, false)
	if err != nil {
		return nil, err
	}

	var unit *cataloguev1.Unit

	for _, existing := range units {
		if existing.GetAbbreviation() == "pc" {
			unit = existing
		}
	}

	if unit == nil {
		if unit, err = api.CreateUnit(ctx, garageID, "Piece", "pc"); err != nil {
			return nil, err
		}
	}

	existing, err := api.ListParts(ctx, garageID, url.Values{"pageSize": {strconv.Itoa(pageSize)}})
	if err != nil {
		return nil, err
	}

	bySKU := make(map[string]*cataloguev1.Part, len(existing))
	for _, part := range existing {
		bySKU[part.GetSku()] = part
	}

	parts := make([]Part, 0, len(pool.Parts))

	for _, spec := range pool.Parts {
		part, found := bySKU[spec.SKU]
		if !found {
			if part, err = api.CreatePart(ctx, garageID, &cataloguev1.Part{
				Sku: spec.SKU, Name: spec.Name, BaseUnitId: unit.GetId(), SalePrice: int64(spec.SalePrice),
			}); err != nil {
				return nil, fmt.Errorf("create %s: %w", spec.SKU, err)
			}

			if _, err = api.ReceiveStock(ctx, garageID, part.GetId(), strconv.Itoa(profile.OpeningStock), "", int64(profile.UnitCost)); err != nil {
				return nil, fmt.Errorf("receive %s: %w", spec.SKU, err)
			}
		}

		parts = append(parts, Part{ID: part.GetId(), Name: spec.Name, SalePrice: int64(spec.SalePrice)})
	}

	return parts, nil
}

// seedJobs creates the pool's jobs; job i suggests part i, so the job picker
// adds a labor line and a stocked part line.
func seedJobs(ctx context.Context, api *client.Client, garageID string, pool *testdata.LoadTest, parts []Part) ([]Job, error) {
	existing, err := api.ListJobs(ctx, garageID, url.Values{"pageSize": {strconv.Itoa(pageSize)}})
	if err != nil {
		return nil, err
	}

	byName := make(map[string]*cataloguev1.Job, len(existing))
	for _, job := range existing {
		byName[job.GetName()] = job
	}

	jobs := make([]Job, 0, len(pool.Jobs))

	for i, spec := range pool.Jobs {
		job, found := byName[spec.Name]
		if !found {
			if job, err = api.CreateJob(ctx, garageID, &cataloguev1.Job{
				Name: spec.Name, LaborPrice: int64(spec.Price), DurationMinutes: 30,
				Parts: []*cataloguev1.JobPart{{PartId: parts[i%len(parts)].ID, Quantity: "1"}},
			}); err != nil {
				return nil, fmt.Errorf("create %q: %w", spec.Name, err)
			}
		}

		jobs = append(jobs, Job{ID: job.GetId(), Name: spec.Name, LaborPrice: int64(spec.Price)})
	}

	return jobs, nil
}

// seedCustomers creates the garage's customers and their vehicles, skipping the
// ones that exist (matched by name and plate), and returns all of them.
func seedCustomers(
	ctx context.Context, api *client.Client, garageID string, profile testdata.LoadProfile, pool *testdata.LoadTest, n int,
) ([]Customer, error) {
	customersByName, err := listAllCustomers(ctx, api, garageID)
	if err != nil {
		return nil, err
	}

	vehiclesByPlate, err := listAllVehicles(ctx, api, garageID)
	if err != nil {
		return nil, err
	}

	customers := make([]Customer, 0, profile.CustomersPerGarage)

	for i := range profile.CustomersPerGarage {
		name := customerName(pool, n, i)

		customer, found := customersByName[name]
		if !found {
			if customer, err = api.CreateCustomer(ctx, garageID, &customerv1.Customer{
				FullName: name, Phone: new(customerPhone(n, i)), PersonalDataConsent: true,
			}); err != nil {
				return nil, fmt.Errorf("create customer %q: %w", name, err)
			}
		}

		entry := Customer{ID: customer.GetId()}

		for j := range profile.VehiclesPerCustomer {
			licensePlate := plate(profile, n, i, j)

			vehicle, found := vehiclesByPlate[licensePlate]
			if !found {
				spec := pool.Vehicles[(i+j+n)%len(pool.Vehicles)]
				if vehicle, err = api.CreateVehicle(ctx, garageID, &customerv1.Vehicle{
					CustomerId: customer.GetId(), LicensePlate: licensePlate,
					Make: new(spec.Make), Model: new(spec.Model), Year: new(int32(spec.Year)), //nolint:gosec // model years are small
				}); err != nil {
					return nil, fmt.Errorf("create vehicle %s: %w", licensePlate, err)
				}
			}

			entry.VehicleIDs = append(entry.VehicleIDs, vehicle.GetId())
		}

		customers = append(customers, entry)
	}

	return customers, nil
}

func listAllCustomers(ctx context.Context, api *client.Client, garageID string) (map[string]*customerv1.Customer, error) {
	all := map[string]*customerv1.Customer{}

	for page := 1; ; page++ {
		data, total, err := api.ListCustomersPage(ctx, garageID, page, pageSize)
		if err != nil {
			return nil, err
		}

		for _, customer := range data {
			all[customer.GetFullName()] = customer
		}

		if page*pageSize >= total {
			return all, nil
		}
	}
}

func listAllVehicles(ctx context.Context, api *client.Client, garageID string) (map[string]*customerv1.Vehicle, error) {
	all := map[string]*customerv1.Vehicle{}

	for page := 1; ; page++ {
		data, total, err := api.ListVehiclesPage(ctx, garageID, page, pageSize)
		if err != nil {
			return nil, err
		}

		for _, vehicle := range data {
			all[vehicle.GetLicensePlate()] = vehicle
		}

		if page*pageSize >= total {
			return all, nil
		}
	}
}
