The Janitor requires a bit of overriding in overlays.

At the very least:
1. You must override the `JANITOR_RESOURCE_TYPES` environment variable in the Deployment to contain the list of resource types this Janitor should manage.
2. You must ensure the Service Account has necessary credentials to clean up the resources.

You will likely also want to increase the number of replicas.

If you want to have multiple kinds of Janitors (perhaps servicing different resource types), you can create separate overlays for each Janitor. Consult the example overlay for details.
