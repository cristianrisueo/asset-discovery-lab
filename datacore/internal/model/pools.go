package model

import (
	"fmt"
	"math/rand/v2"
)

// Sites is the site pool (spec §5.3).
var Sites = []string{"Alicante", "Madrid", "Barcelona", "Valencia", "Sevilla", "Bilbao"}

// WeightedValue is one value of a weighted pool.
type WeightedValue struct {
	Value  string
	Weight int
}

// ClientOS is the OS pool of laptops and desktops (spec §5.3).
var ClientOS = []WeightedValue{
	{"Windows 11", 60}, {"Windows 10", 15}, {"macOS 15", 20}, {"Ubuntu 24.04", 5},
}

// ServerOS is the OS pool of servers (spec §5.3).
var ServerOS = []WeightedValue{{"Ubuntu 24.04", 60}, {"Windows Server 2022", 40}}

// OSPool returns the OS pool that fits an asset id.
func OSPool(assetID string) []WeightedValue {
	if len(assetID) >= 3 && assetID[:3] == "SV-" {
		return ServerOS
	}
	return ClientOS
}

// PickWeighted draws a value of pool with its weights.
func PickWeighted(r *rand.Rand, pool []WeightedValue) string {
	total := 0
	for _, v := range pool {
		total += v.Weight
	}
	n := r.IntN(total)
	for _, v := range pool {
		if n < v.Weight {
			return v.Value
		}
		n -= v.Weight
	}
	return pool[len(pool)-1].Value
}

// GroupNames are the names of the seeded groups G-01 to G-08 (spec §5.3).
var GroupNames = []string{
	"Dirección", "Ingeniería", "Ventas", "Marketing",
	"Finanzas", "Soporte", "Personas", "Operaciones",
}

// GroupRenamePool is the fixed pool the generator renames groups from (spec §7.6).
var GroupRenamePool = append(append([]string(nil), GroupNames...),
	"Legal", "Compras", "Calidad", "Logística", "Innovación", "Datos", "Seguridad", "Comunicación")

// FirstNames and Surnames generate user names deterministically (spec §5.3).
var (
	FirstNames = []string{
		"Ana", "María", "Lucía", "Carmen", "Laura", "Marta", "Elena", "Sara", "Paula", "Isabel",
		"Cristina", "Pilar", "Rosa", "Nuria", "Raquel", "Irene", "Alba", "Claudia", "Sofía", "Julia",
		"Antonio", "Manuel", "José", "Francisco", "David", "Juan", "Javier", "Daniel", "Carlos", "Jesús",
		"Alejandro", "Miguel", "Rafael", "Pablo", "Sergio", "Fernando", "Jorge", "Alberto", "Luis", "Álvaro",
	}
	Surnames = []string{
		"García", "Rodríguez", "González", "Fernández", "López", "Martínez", "Sánchez", "Pérez", "Gómez", "Martín",
		"Jiménez", "Ruiz", "Hernández", "Díaz", "Moreno", "Muñoz", "Álvarez", "Romero", "Alonso", "Gutiérrez",
		"Navarro", "Torres", "Domínguez", "Vázquez", "Ramos", "Gil", "Ramírez", "Serrano", "Blanco", "Molina",
		"Morales", "Suárez", "Ortega", "Delgado", "Castro", "Ortiz", "Rubio", "Marín", "Sanz", "Iglesias",
	}
)

// RandomName draws "First Surname".
func RandomName(r *rand.Rand) string {
	return FirstNames[r.IntN(len(FirstNames))] + " " + Surnames[r.IntN(len(Surnames))]
}

// The fixed example of the project map (spec §5.3).
const (
	ExampleAssetID   = "LT-0042"
	ExampleUserID    = "U-0117"
	ExampleUserName  = "Ana Ruiz"
	ExampleGroupID   = "G-03"
	ExampleGroupName = "Ventas"
	ExampleOS        = "Windows 11"
	ExampleSite      = "Alicante"
)

// Seed size limits keep seeded ids out of the reserved and phantom ranges (spec §5.4).
const (
	MaxSeedGroups      = 89   // G-01 to G-89; G-90 to G-99 are reserved
	MaxSeedUsers       = 8999 // U-0001 to U-8999; U-9000 and above are reserved
	MaxSeedAssetsPerPx = 7999 // LT-0001 to LT-7999; LT-8000 and above are phantom or reserved
)

// SeedCounts is the size of the seed dataset (spec §7.10). Assets are split
// 60/30/10 into laptops, desktops and servers.
type SeedCounts struct {
	Groups, Users, Assets int
}

// Split returns how many laptops, desktops and servers the seed holds.
func (c SeedCounts) Split() (laptops, desktops, servers int) {
	laptops = c.Assets * 60 / 100
	desktops = c.Assets * 30 / 100
	servers = c.Assets - laptops - desktops
	return laptops, desktops, servers
}

// Validate checks that every seeded id stays out of the reserved ranges.
func (c SeedCounts) Validate() error {
	lt, dt, sv := c.Split()
	switch {
	case c.Groups < 1 || c.Groups > MaxSeedGroups:
		return fmt.Errorf("SEED_GROUPS %d is outside [1, %d]", c.Groups, MaxSeedGroups)
	case c.Users < 1 || c.Users > MaxSeedUsers:
		return fmt.Errorf("SEED_USERS %d is outside [1, %d]", c.Users, MaxSeedUsers)
	case c.Assets < 1 || lt > MaxSeedAssetsPerPx || dt > MaxSeedAssetsPerPx || sv > MaxSeedAssetsPerPx:
		return fmt.Errorf("SEED_ASSETS %d puts seeded ids into the reserved ranges", c.Assets)
	}
	return nil
}

// GroupIDs lists the seeded group ids.
func (c SeedCounts) GroupIDs() []string { return ids("G-%02d", c.Groups) }

// UserIDs lists the seeded user ids.
func (c SeedCounts) UserIDs() []string { return ids("U-%04d", c.Users) }

// AssetIDs lists the seeded asset ids: laptops, then desktops, then servers.
func (c SeedCounts) AssetIDs() []string {
	lt, dt, sv := c.Split()
	out := ids("LT-%04d", lt)
	out = append(out, ids("DT-%04d", dt)...)
	return append(out, ids("SV-%04d", sv)...)
}

func ids(format string, n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = fmt.Sprintf(format, i+1)
	}
	return out
}
