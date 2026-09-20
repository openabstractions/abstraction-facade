package resolution

// UpgradeInProgress: default discovery found the installed runtime's endpoint
// absent, and activating it was refused because an installer is replacing that
// installation. The Error's cause wraps bootstrap.ErrUpgradeInProgress. Retry
// when the installation finishes.
const UpgradeInProgress ErrorStatus = "upgrade_in_progress"
