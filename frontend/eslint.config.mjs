import nextConfig from "eslint-config-next";

const eslintConfig = [
    ...nextConfig,
    {
        files: ["**/*.ts", "**/*.tsx"],
        rules: {
            "@typescript-eslint/no-explicit-any": "error",
            "no-restricted-syntax": ["error", {
                selector: "TSUnknownKeyword",
                message: "Use a concrete DTO, a typed registry, or validated JsonValue at dynamic JSON boundaries.",
            }, {
                selector: "CallExpression[callee.type='Identifier'][callee.name=/^use.*Store$/][arguments.length=0]",
                message: "Select the state or actions this component uses instead of subscribing to the entire store.",
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
