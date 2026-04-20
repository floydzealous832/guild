package mcp

// All lore tools now live in internal/lore as registry-generated
// Command specs. Registration happens in register.go via BindMCP calls
// on the respective *Command values. See
// docs/architecture/COMMAND_REGISTRY.md.
//
// Registered verbs: lore_appraise, lore_study, lore_oath, lore_list,
// lore_dossier, lore_inscribe, lore_reforge, lore_update, lore_inquest,
// lore_meld, lore_commune, lore_seal, lore_catalog, lore_link,
// lore_echoes, lore_whispers.
