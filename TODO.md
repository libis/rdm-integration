## General Remarks
- **Action Point:**[] Add “integration dashboard” under edit dataset button as external tool
- Non-allowed characters => it can be confusing if you don’t know which characters are not allowed.
    - **Action Point:**[] Explore provide a warning beforehand somewhere or a clearer warning with specifically what the issue is. Split which files exceed the file size limit & which files contain unsupported characters (give list of what characters are or aren’t supported).
- When you add files to an existing dataset, you get the option to upload files via Globus transfer. However, the button redirects to the integration dashboard (with Globus preselected, but not clear).
    - **Action Point:**[] Suggestion: rename button to “Upload from other source” & guidance to “Upload files and folder structures from SharePoint, ManGO, Globus, GitHub etc. using integration dashboard. This method is recommended for large file transfers and complex folder structures. (Using it will cancel any other types of uploads in progress on this page.)”
- After clicking on Authorize to select your source, it is not immediately clear that you have to expand the data/code source again (even if you have selected your source previously)
    - **Action Point:**[] It should be uncollapsed after an authorization. Or both expanded to start with. Maybe option to collapse isn’t necessary, though then there is a hover message necessary to indicate what’s missing when someone tries to got to the “connect & compare” button.
    - **Action Point:**[] Also; collapse or uncollapse shouldn’t give the circle animation.
- (on production as well): GitHub authorize doesn’t work (22.09.2025)

## First Screen

- ‘Select file actions’ => what does it mean? Perhaps ‘bulk action’?
    - **Action Point:**[] Title for each page centered between “Return” and the next button. Font as large as “select file actions currently on page 1” in black. Move action buttons to a new row:
        - “Select source and destination”
        - “Select file actions”
        - “Select metadata actions”
        - “Transfer preview” & “Transfer status” once submitted
- Add path / folder name to Source (as is already the case with DOI on the right side)
    - **Action Point:**[] Add selected folder name in between brackets (like the doi)
- Suggestion: use the action icons (delete, copy, update) instead of the checkboxes. This because:
    - Hover text not logical => Is ‘Do nothing with this file’ expected upon clicking? No.
        - **Action Point:**[] Remove the hover
    - The delete icon  and the copy icon  are actually deactivated when clicking on them
        - Can be confusing, but no immediate improvement possible
- Return => back to the previous step? It takes a lot of clicks to get back to the previous point, if you e.g. want to go one level up and select other/different files
    - **Action Point:**[] Is there a way to go back to the select source and destination page without resetting the folder selection and destination selection?. Alternative: have button on this page say “Restart”
    
## Second Screen (Overview of Actions)

- When clicking on Return I would expect to go back to my previous selection. Instead, the whole folder is shown without any files being selected. Use case: if you forgot to select only one file you have to go over the whole process again.
    - **Action Point:**[] Is it possible to copy the previous selection back into the file selection page?
- Show a ‘transfer completed’ message? Otherwise, you only get one if you tick the checkbox to receive an email.
    - **Action Point:**[] Add a progress bar with file count (not looking at file size)
    - **Action Point:**[] For Globus: Have it say “Transfer submitted in Globus, progress can be checked in the dataset page”.
    - For Globus: you get a checkmark when the file has been submitted, not indicating that the transfer is complete --> maybe indicate this more clearly.
        - **Action Point:**[] Also not getting the pop-up about the email?
