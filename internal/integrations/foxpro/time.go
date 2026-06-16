package foxpro

import "time"

func billingTimeNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
