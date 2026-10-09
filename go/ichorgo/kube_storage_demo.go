package ichorgo

// The demo's claims, as the checkup's demo volumes describe them: one nearly full Postgres
// volume, a filling Prometheus one, a claim waiting for its class.
func demoStorage(namespace string) func() kubeStorage {
	return func() kubeStorage {
		const gi = 1 << 30

		claim := func(ns, name, class, provisioner string, capacity, usedPercent float64, pods ...string) storageClaim {
			c := storageClaim{
				Namespace: ns, Name: name, Phase: "Bound", StorageClass: class, Provisioner: provisioner,
				Volume: "pvc-" + name, ReclaimPolicy: "Delete", AccessModes: []string{"ReadWriteOnce"},
				Capacity: capacity, Pods: append([]string{}, pods...),
			}
			if usedPercent > 0 {
				c.Measured, c.Used, c.UsedPercent, c.InodesPercent = true, capacity*usedPercent/100, usedPercent, usedPercent/4
			}

			c.ManagedBy = managedByOf(nil, provisioner)

			return c
		}

		claims := []storageClaim{
			claim("demo", "data-postgres-0", "longhorn", "driver.longhorn.io", 20*gi, 96.5, "postgres-0"),
			claim("demo", "uploads", "longhorn", "driver.longhorn.io", 50*gi, 21, "hello-ichor-7d9c5-abcde", "hello-ichor-7d9c5-fghij"),
			claim("demo", "search-data", "longhorn", "driver.longhorn.io", 10*gi, 44, "worker-6f4b8-pqrst"),
			claim("monitoring", "prometheus-db", "local-path", "rancher.io/local-path", 30*gi, 87, "prometheus-0"),
		}

		pending := claim("demo", "cache", "fast", "", 5*gi, 0)
		pending.Phase, pending.Volume, pending.ReclaimPolicy = "Pending", "", ""
		claims = append(claims, pending)

		for i := range claims {
			claims[i].Level = storageLevel(claims[i])
		}

		claims = inNamespace(claims, namespace, func(c storageClaim) string { return c.Namespace })
		sortClaims(claims)

		return kubeStorage{Claims: claims}
	}
}
