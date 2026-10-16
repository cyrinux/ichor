package ichorgo

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// sideroServerObject is a Sidero Metal Server (metal.sidero.dev/v1alpha2, cluster-scoped). Its
// spec.bmc names the Secret holding the IPMI credentials: the struct leaves it out, so it is
// never decoded nor shown.
type sideroServerObject struct {
	Metadata capiMeta `json:"metadata"`
	Spec     struct {
		Hostname string `json:"hostname"`
		Accepted bool   `json:"accepted"`
		Cordoned bool   `json:"cordoned"`
		Hardware *struct {
			Compute *sideroCompute `json:"compute"`
			Memory  *sideroMemory  `json:"memory"`
		} `json:"hardware"`
	} `json:"spec"`
	Status struct {
		Ready     bool   `json:"ready"`
		InUse     bool   `json:"inUse"`
		IsClean   bool   `json:"isClean"`
		Power     string `json:"power"`
		Addresses []struct {
			Address string `json:"address"`
		} `json:"addresses"`
	} `json:"status"`
}

// sideroCompute is a Server's processor inventory (spec.hardware.compute).
type sideroCompute struct {
	TotalCoreCount int `json:"totalCoreCount"`
	ProcessorCount int `json:"processorCount"`
	Processors     []*struct {
		ProductName string `json:"productName"`
	} `json:"processors"`
}

// sideroMemory is a Server's memory inventory (spec.hardware.memory).
type sideroMemory struct {
	TotalSize string `json:"totalSize"`
	Modules   []*struct {
		Size int64 `json:"size"` // MiB (SMBIOS "MB")
	} `json:"modules"`
}

// sideroServerClassObject is a ServerClass: the Servers it matches, free and in use.
type sideroServerClassObject struct {
	Metadata capiMeta `json:"metadata"`
	Status   struct {
		ServersAvailable []string `json:"serversAvailable"`
		ServersInUse     []string `json:"serversInUse"`
	} `json:"status"`
}

// sideroRows maps the Servers and ServerClasses, each Server naming the classes that list it.
func sideroRows(servers []sideroServerObject, classes []sideroServerClassObject) ([]sideroServer, []sideroServerClass) {
	classesOf := map[string][]string{}
	classRows := []sideroServerClass{}

	for _, c := range classes {
		classRows = append(classRows, sideroServerClass{
			Name: c.Metadata.Name, Available: len(c.Status.ServersAvailable), InUse: len(c.Status.ServersInUse),
		})

		for _, s := range slices.Concat(c.Status.ServersAvailable, c.Status.ServersInUse) {
			if !slices.Contains(classesOf[s], c.Metadata.Name) {
				classesOf[s] = append(classesOf[s], c.Metadata.Name)
			}
		}
	}

	rows := []sideroServer{}
	for _, s := range servers {
		row := sideroServerRow(s)
		row.ServerClasses = nonNil(classesOf[row.Name])
		slices.Sort(row.ServerClasses)
		rows = append(rows, row)
	}

	return rows, classRows
}

func sideroServerRow(s sideroServerObject) sideroServer {
	row := sideroServer{
		Name:          s.Metadata.Name,
		Hostname:      s.Spec.Hostname,
		Accepted:      s.Spec.Accepted,
		Cordoned:      s.Spec.Cordoned,
		Ready:         s.Status.Ready,
		InUse:         s.Status.InUse,
		IsClean:       s.Status.IsClean,
		Power:         cmp.Or(s.Status.Power, "unknown"),
		Addresses:     []string{},
		ServerClasses: []string{},
	}

	for _, a := range s.Status.Addresses {
		if a.Address != "" && !slices.Contains(row.Addresses, a.Address) {
			row.Addresses = append(row.Addresses, a.Address)
		}
	}

	if hw := s.Spec.Hardware; hw != nil {
		row.CPU = sideroCPU(hw.Compute)
		row.MemoryBytes = sideroMemoryBytes(hw.Memory)
	}

	return row
}

// sideroCPU is "2 × <model>, 32 cores", "" when the compute inventory is empty.
func sideroCPU(c *sideroCompute) string {
	if c == nil {
		return ""
	}

	model := ""
	for _, p := range c.Processors {
		if p != nil && p.ProductName != "" {
			model = strings.TrimSpace(p.ProductName)

			break
		}
	}

	var parts []string
	switch {
	case model != "" && c.ProcessorCount > 1:
		parts = append(parts, strconv.Itoa(c.ProcessorCount)+" × "+model)
	case model != "":
		parts = append(parts, model)
	}
	if c.TotalCoreCount > 0 {
		parts = append(parts, strconv.Itoa(c.TotalCoreCount)+" cores")
	}

	return strings.Join(parts, ", ")
}

// sideroMemoryBytes is the memory modules' total, else totalSize ("64 GB", read as GiB), 0
// when unknown.
func sideroMemoryBytes(m *sideroMemory) int64 {
	if m == nil {
		return 0
	}

	var mib int64
	for _, mod := range m.Modules {
		if mod != nil {
			mib += mod.Size
		}
	}
	if mib > 0 {
		return mib << 20
	}

	value, unit, _ := strings.Cut(strings.TrimSpace(m.TotalSize), " ")
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}

	switch strings.ToUpper(unit) {
	case "TB":
		return n << 40
	case "GB":
		return n << 30
	case "MB":
		return n << 20
	default:
		return 0
	}
}
