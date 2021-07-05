# Orchestrator

Setup a script that will handle requests sent to it, attaching a correlation ID to them.
It will then send the request to either Wordpress script or Regular script.
It will then respond with whatever response it receives. <br />

# Wordpress

This script will handle requests from the Orchestrator and will query wordpress sites for a specific check, i.e. plugins, users or site settings.
It will filter the response it receives before sending it back to Orchestrator. <br />

#Regular

Basic http setup has been done for this one, but requesting sites has not been added yet. <br />
