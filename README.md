# Orchestrator

## v1.0<br />
Setup a script that will handle requests sent to it, attaching a correlation ID to them.
It will then send the request to either Wordpress script or Regular script.
It will then respond with whatever response it receives from them. <br />
## v1.1<br />
Fix error responses failing. <br />
Removed errors sent to client. <br />
Added authorization token header that is required for every request. <br />
Also added this authorization token header for between Orchestrator and Wordpress/Regular. <br />
## v2.0<br />
Added check for 'http://', 'https://' and 'www.' within start of the URL and stripped them out. These are added/used within code and are unneeded input. <br />
Fixed bugs. <br />

# Regular

## v1.0<br />
Basic http setup has been done for this one, but requesting sites has not been added yet. <br />
## v1.1<br />
Fix error responses failing. <br />
Removed errors sent to client. <br />
Requesting sites has been added now. Only sends a basic check for Status OK. <br />
## v2.0<br />
Added logging using mongoDB. <br />
mongoDB: Will create an entry to the collection for each request with it's URL and status code. <br />
Added Correlation ID to mongoDB, to keep with how wordpress logging works. <br />
Changed check from checking for 200 to checking for 200-299. <br />
Fixed bugs. <br />

# Wordpress

## v1.0<br />
This script will handle requests from the Orchestrator and will query wordpress sites for a specific check, i.e. plugins, users or site settings.
It will filter the response it receives before sending it back to Orchestrator. <br />
## v1.1<br />
Fix error responses failing. <br />
Removed errors sent to client. <br />
Added a basic check which will send a get request and respond with a Status OK or a specific Error Status. <br />
## v2.0<br />
Added logging using mongoDB. <br />
mongoDB: Will create an entry to the collection for each check with it's URL and status code. <br />
Added Correlation ID to mongoDB, to keep track of multiple checks in a single request. <br />
Changed check from checking for 200 to checking for 200-299. <br />
Removed basic check, made it do a basic check by default. <br />
Fixed bugs, no longer panics and is able to respond when given incorrect nonce and cookie details. <br />

# Instructions

## v2.0 <br />
Send POST requests with endpoint /orch. i.e. example.com/orch <br />
Send a single JSON object with: <br />

```JSON
{
  "url": "site URL here",
  "platform": "put 'wordpress' or 'regular' here depending on the site type",
  "check": ["type of check done, i.e. 'users', 'plugins' and 'config'"]
}
```
<br />

A basic check is done by default with 'regular' or 'wordpress' websites whilst the other three ('users', 'plugins' and 'config') are restricted to solely 'wordpress' websites. <br /><br />
Here is an example of a 'wordpress' request's JSON:<br />

```JSON
{
  "url": "example.com",
  "platform": "wordpress",
  "check": ["users", "plugins"]
}
```
<br />
Here is an example of a 'regular' request's JSON (check is left with empty value):<br />

```JSON
{
  "url": "example.com",
  "platform": "regular",
  "check": [""]
}
```
