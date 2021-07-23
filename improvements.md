# Possible Improvements or Changes
## Complex Changes: <br />
- [ ] Cache API results for a certain period of time in Orchestrator, reduces cost of sending and receiving API requests.
- [ ] Improvement on Caching, make sure credentials are valid first.
- [ ] Expand on plugin checks with update checks etc.
## Simple Improvements: <br />
- [x] Add user roles to 'users' output (i.e. "Administrator").
- [x] Add information for 'plugins' output, mainly the version and requirements.
- [x] Incorrect logging of regular checks, if the check fails nothing is logged at all.
- [x] Since regular checks log the same way, simplified by creating a logging function for it.
- [x] Prepare wordpress script for future checks, and for added details to checks.
