# Wordpress

## v1.0<br />
This script will handle requests from the Orchestrator and will query wordpress sites for a specific check, i.e. plugins, users or site settings.
It will filter the response it receives before sending it back to Orchestrator. <br />
## v1.1<br />
Fix error responses failing. <br />
Removed errors sent to client. <br />
Added a basic check which will send a get request and respond with a Status OK or a specific Error Status. <br />

# Regular

## v1.0<br />
Basic http setup has been done for this one, but requesting sites has not been added yet. <br />
## v1.1<br />
Fix error responses failing. <br />
Removed errors sent to client. <br />
Requesting sites has been added now. Only sends a basic check for Status OK. <br />

# Instructions

## v1.1 <br />
Send POST requests with /orch. i.e. example.com/orch <br />
Send a single JSON object with: <br />

```JSON
{
  "url": "site URL here",
  "platform": "put 'wordpress' or 'regular' here depending on the site type",
  "check": ["type of check done, 'basic', 'users', 'plugins' and 'config'"]
}
```
<br /><br />

A 'basic' check is done by 'regular' or 'wordpress' websites whilst the other three ('users', 'plugins' and 'config') are restricted to solely 'wordpress' websites. <br /><br />
Here is an example of a 'wordpress' request's JSON:<br />

```JSON
{
  "url": "example.com",
  "platform": "wordpress",
  "check": ["users", "plugins"]
}
```
