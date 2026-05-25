# BeiShan iOS App

This is the first actual Xcode iPhone app shell for BeiShan.

It compiles as a thin remote client and reuses the shared source files from:

- `BeiShan/clients/apple-core/Sources/BeiShanClientCore`

Current focus:
- remote relay access
- remote chat
- remote health
- remote session list and session history loading
- explicit memory write
- workspace skills and workflow templates inspection
- editable connection settings for ECS relay or custom endpoints
- image upload to remote BeiShan
- file upload to remote BeiShan
- microphone recording and remote STT dispatch
- realtime voice capability detection and session bootstrap entry

Deliberately excluded for now:
- local runtime bootstrap
- direct mobile ownership of voice reasoning/runtime state
- rich upload gallery/history management

## Quick Start On iPhone

1. Connect your iPhone to this Mac with a cable and tap `Trust` on the phone if prompted.
2. Open:
   - `BeiShan_iOS_client/BeiShanIOSApp/BeiShanIOSApp.xcodeproj`
3. In Xcode, click the project root `BeiShanIOSApp`, then:
   - open `Signing & Capabilities`
   - choose your personal Apple ID team
4. If Xcode says the bundle id is already taken, change:
   - `com.beishan.iosclient`
   - to something unique like `com.dc.beishan.iosclient`
5. Select your iPhone as the run destination and press `Run`.
6. Enter your own relay URL and credentials in the app's connection settings before the first real remote test.
7. If iPhone blocks the developer app on first launch, go to:
   - `Settings > General > VPN & Device Management`
   - trust your developer certificate

## What Already Works

- remote health check
- remote chat
- remote session list and history
- explicit memory write
- image upload
- file upload
- local audio recording -> remote STT -> reply
- realtime voice transport capability probing
- realtime voice session bootstrap shell

## Thin Client Boundary

This iPhone app is only a projection of the Mac runtime.

It does not own:
- session truth
- long-term memory truth
- planning
- routing
- model execution
