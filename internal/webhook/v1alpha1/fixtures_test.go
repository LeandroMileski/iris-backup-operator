package v1alpha1

// Repeated fixture values. goconst flags a string used three or more times.
const (
	labelApp            = "app"
	appIris             = "iris"
	imageBusybox        = "busybox"
	cmdEcho             = "echo"
	scheduleNotCron     = "not-a-cron"
	scheduleOutOfRange  = "60 0 * * *"
	scheduleOneAM       = "0 1 * * *"
	fieldTargetSelector = "spec.targetSelector"
	fieldSchedule       = "spec.schedule"
	fieldRetention      = "spec.retention"
)
