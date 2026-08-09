// Package fakedrive holds CI-only integration coverage for the Linux Google
// Drive mirror path.
//
// Nothing here is linked into the shipped application. Tests stand in for
// Drive with a local directory behind an rclone alias remote so pull requests
// can prove host/guest setup, nested-folder resolution, and bisync transfer
// without OAuth, network access to Google, or a real Drive account.
package fakedrive
