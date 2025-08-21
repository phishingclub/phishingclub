# Description
The `model` folder represents the core data structures. It includes:

- Factories for creating and initializing entities
- Type definitions for all domain objects
- Validation logic to ensure data integrity
- Custom marshalling/unmarshalling for JSON handling
- Request/Response structures for API endpoints
- Helper methods for data transformation
- Business rules and constraints
- Data transfer objects (DTOs) for external communication

The members mainly use `nullable.Nullable` to convey if a field is set so we know
if it should be updated etc.

The structures in this folder serve as the contract between the API layer and the database layer, handling all necessary data transformations and validations before persistence or transmission.

Files affixed with `View` are read-only models meant only for outgoing API data,
this could be joined data from a database.
