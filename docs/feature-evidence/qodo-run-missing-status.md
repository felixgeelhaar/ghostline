# Qodo run with missing status

This Chrome screenshot shows the running Ghostline structured-review component with a prepared replay fixture. The fixture includes a Qodo finding and a review run with a native ID and revision but no source status. Ghostline keeps the finding and run visible and labels the missing status explicitly.

The fixture is sample data, not a live Qodo response. The screenshot documents the UI behavior; backend and frontend regression tests verify that the API/domain boundaries accept the statusless run without inventing a status.
