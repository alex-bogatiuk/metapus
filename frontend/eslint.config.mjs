import nextConfig from "eslint-config-next";

const sharedRestrictedSyntax = [{
    selector: "TSUnknownKeyword",
    message: "Use a concrete DTO, a typed registry, or validated JsonValue at dynamic JSON boundaries.",
}, {
    selector: "CallExpression[callee.type='Identifier'][callee.name=/^use.*Store$/][arguments.length=0]",
    message: "Select the state or actions this component uses instead of subscribing to the entire store.",
}];

// A configuration (configuration/<name>/) adds its parts to the platform
// registries; app/ is the composition root that registers it. Platform code
// therefore never imports a configuration. Playwright specs (tests/) test the
// assembled application and may use its configuration's types.
const configurationImport = /^(@\/|(\.\.?\/)+)configuration(\/|$)/;
// esquery ends a regex at the first "/", so selectors spell it \x2F.
const configurationImportSelector = `/${configurationImport.source.replaceAll("\\/", "\\x2F")}/`;
const configurationBoundaryMessage =
    "Platform code must not import a configuration. Add an extension point to a platform registry and register the configuration's part from configuration/<name>/.";

const eslintConfig = [
    ...nextConfig,
    {
        files: ["**/*.ts", "**/*.tsx"],
        rules: {
            "@typescript-eslint/no-explicit-any": "error",
            "no-restricted-syntax": ["error", ...sharedRestrictedSyntax],
        },
    },
    {
        files: ["**/*.ts", "**/*.tsx"],
        ignores: ["app/**", "configuration/**", "tests/**"],
        rules: {
            "no-restricted-imports": ["error", {
                patterns: [{ regex: configurationImport.source, message: configurationBoundaryMessage }],
            }],
            "no-restricted-syntax": ["error", ...sharedRestrictedSyntax, {
                selector: `ImportExpression[source.value=${configurationImportSelector}]`,
                message: configurationBoundaryMessage,
            }, {
                selector: `TSImportType Literal[value=${configurationImportSelector}]`,
                message: configurationBoundaryMessage,
            }],
        },
    },
    {
        ignores: [
            ".next/**",
            "out/**",
            "build/**",
            "node_modules/**",
            "playwright-report/**",
            "test-results/**",
            "*.tsbuildinfo",
            "lint*.json",
            "*.log"
        ]
    }
];

export default eslintConfig;
