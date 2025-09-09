// ... existing code ...
if len(conf.AppConfigSource) > 0 {
    log.Info("Initializing app config source...")
    configWatcher = initConfigSource(context.Background()) // 修改这里
} else {
    log.Info("No app config source configured")
}
// ... existing code ...