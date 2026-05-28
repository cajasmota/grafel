      ******************************************************************
      * TAXRULES.CPY — shared tax-bracket copybook (data only).        *
      * Resolved on disk by the COPY resolver (#2838) so the IMPORTS    *
      * edge from a using program binds to this file, raising           *
      * import_resolution_quality from partial to full.                 *
      ******************************************************************
       01  TAX-RULES.
           05  TR-BRACKET        PIC 9(02).
           05  TR-RATE           PIC 9V9(04).
           05  TR-CAP            PIC 9(07)V99.
